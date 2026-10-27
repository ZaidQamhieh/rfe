package expo.modules.rfetransport

import java.io.BufferedInputStream
import java.io.InputStream
import java.io.OutputStream
import java.net.InetAddress
import java.net.ServerSocket
import java.net.Socket
import java.security.SecureRandom
import java.util.concurrent.ConcurrentHashMap
import java.util.concurrent.Executors

/**
 * Port of VideoLoopbackProxy: a 127.0.0.1-only HTTP bridge so the platform media player can stream
 * a remote file without knowing about the agent's pinning or bearer auth. Each instance serves one
 * file at a random one-use path (another local app that finds the port still can't read the file,
 * PR-27), accepts only GET/HEAD, forwards Range for seeking, and relays the pinned response.
 */
class MediaProxy private constructor(
  private val server: ServerSocket,
  private val url: String,
  private val headers: Map<String, String>,
  private val pin: String,
  val path: String,
) {
  private val pool = Executors.newCachedThreadPool { r -> Thread(r, "rfe-media-proxy").apply { isDaemon = true } }
  @Volatile private var closed = false

  val port: Int get() = server.localPort

  init {
    pool.execute {
      while (!closed) {
        val s = try { server.accept() } catch (_: Exception) { break }
        pool.execute { handle(s) }
      }
    }
  }

  private fun handle(sock: Socket) {
    sock.use { s ->
      s.soTimeout = 30_000
      val input = BufferedInputStream(s.getInputStream())
      val out = s.getOutputStream()
      val requestLine = readLine(input) ?: return
      val reqHeaders = HashMap<String, String>()
      while (true) {
        val line = readLine(input) ?: return
        if (line.isEmpty()) break
        val i = line.indexOf(':')
        if (i > 0) reqHeaders[line.substring(0, i).trim().lowercase()] = line.substring(i + 1).trim()
      }
      val parts = requestLine.split(' ')
      val method = parts.getOrNull(0) ?: ""
      val target = parts.getOrNull(1) ?: ""
      if ((method != "GET" && method != "HEAD") || target != path) {
        writeStatus(out, 404, "Not Found", emptyMap(), 0)
        return
      }
      forward(method, reqHeaders["range"], out)
    }
  }

  private fun forward(method: String, range: String?, out: OutputStream) {
    val h = headers.toMutableMap()
    if (range != null) h["Range"] = range
    val call = try {
      PinnedHttp.newGetCall(url, h, pin)
    } catch (_: Exception) {
      writeStatus(out, 502, "Bad Gateway", emptyMap(), 0)
      return
    }
    try {
      call.execute().use { r ->
        val relay = LinkedHashMap<String, String>()
        for (name in RELAYED) r.header(name)?.let { relay[name] = it }
        val len = r.header("content-length")?.toLongOrNull()
        writeStatus(out, r.code, r.message.ifEmpty { "OK" }, relay, len)
        if (method == "HEAD") return
        r.body?.byteStream()?.copyTo(out, 64 * 1024)
        out.flush()
      }
    } catch (_: Exception) {
      // Best effort: the player reports a broken stream as its own playback error.
    }
  }

  private fun writeStatus(out: OutputStream, code: Int, reason: String, headers: Map<String, String>, length: Long?) {
    val sb = StringBuilder("HTTP/1.1 $code $reason\r\n")
    for ((k, v) in headers) if (k != "content-length") sb.append("$k: $v\r\n")
    if (length != null) sb.append("content-length: $length\r\n")
    sb.append("connection: close\r\n\r\n")
    out.write(sb.toString().toByteArray(Charsets.ISO_8859_1))
    out.flush()
  }

  fun close() {
    closed = true
    try { server.close() } catch (_: Exception) {}
    pool.shutdownNow()
  }

  companion object {
    private val RELAYED = listOf("content-type", "content-length", "content-range", "accept-ranges")
    private val proxies = ConcurrentHashMap<String, MediaProxy>()
    private val random = SecureRandom()

    /** Starts a proxy for one remote file and returns its loopback URL. */
    fun start(id: String, url: String, headers: Map<String, String>, pin: String?): String {
      val normalized = PinnedHttp.normalizeFingerprint(pin) ?: throw PinPolicyViolation("missing or invalid certificate pin")
      stop(id)
      val token = ByteArray(24).also { random.nextBytes(it) }
      val p = MediaProxy(
        ServerSocket(0, 16, InetAddress.getByName("127.0.0.1")),
        url, headers, normalized,
        "/" + token.joinToString("") { "%02x".format(it) },
      )
      proxies[id] = p
      return "http://127.0.0.1:${p.port}${p.path}"
    }

    fun stop(id: String) {
      proxies.remove(id)?.close()
    }

    private fun readLine(input: InputStream): String? {
      val sb = StringBuilder()
      while (true) {
        val b = input.read()
        if (b < 0) return if (sb.isEmpty()) null else sb.toString()
        if (b == '\n'.code) return sb.toString().trimEnd('\r')
        if (sb.length > 8192) return null
        sb.append(b.toChar())
      }
    }
  }
}
