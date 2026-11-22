package expo.modules.rfetransport

import java.io.BufferedInputStream
import java.io.InputStream
import java.io.OutputStream
import java.net.InetAddress
import java.net.ServerSocket
import java.net.Socket
import java.net.SocketTimeoutException
import java.security.MessageDigest
import java.security.SecureRandom
import java.util.concurrent.ConcurrentHashMap
import java.util.concurrent.Executors
import java.util.concurrent.atomic.AtomicInteger

/**
 * Port of VideoLoopbackProxy: a 127.0.0.1-only HTTP bridge so the platform media player can stream
 * a remote file without knowing about the agent's pinning or bearer auth. Each instance serves one
 * file at a random one-use path (another local app that finds the port still can't read the file,
 * PR-27), accepts only GET/HEAD, forwards Range for seeking, and relays the pinned response. The upstream
 * request is always the configured URL: the player cannot add a query or pick another file, and a request
 * addressed to any host name other than loopback is refused. The proxy closes itself after an idle period.
 */
class MediaProxy private constructor(
  private val id: String,
  private val server: ServerSocket,
  private val url: String,
  private val headers: Map<String, String>,
  private val pin: String,
  val path: String,
  private val idleCloseMs: Long,
) {
  private val pool = Executors.newCachedThreadPool { r -> Thread(r, "rfe-media-proxy").apply { isDaemon = true } }
  @Volatile private var closed = false
  private val active = AtomicInteger(0)
  @Volatile private var lastActiveNanos = System.nanoTime()

  val port: Int get() = server.localPort

  init {
    server.soTimeout = idleCloseMs.coerceIn(1L, Int.MAX_VALUE.toLong()).toInt()
    pool.execute {
      while (!closed) {
        val s = try {
          server.accept()
        } catch (_: SocketTimeoutException) {
          // No new connection for a whole idle period: close unless a stream is still being served or just ended.
          val idleNanos = System.nanoTime() - lastActiveNanos
          if (active.get() > 0 || idleNanos < idleCloseMs * 1_000_000L) continue
          proxies.remove(id, this)
          close()
          break
        } catch (_: Exception) { break }
        lastActiveNanos = System.nanoTime()
        active.incrementAndGet()
        pool.execute {
          try { handle(s) } finally {
            lastActiveNanos = System.nanoTime()
            active.decrementAndGet()
          }
        }
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
      // Exactly one space (method, target, version), no other whitespace and no fragment in the target.
      val malformed = requestLine.count { it == ' ' } != 2 || requestLine.any { it.isWhitespace() && it != ' ' } || target.contains('#')
      // The agent's file endpoint takes the file as a query (?path=), so any query is refused rather than forwarded.
      val hasQuery = target.indexOf('?') >= 0
      val hostOk = reqHeaders["host"]?.let { LOOPBACK_HOST.matches(it) } ?: true
      val pathOk = MessageDigest.isEqual(target.toByteArray(Charsets.ISO_8859_1), path.toByteArray(Charsets.ISO_8859_1))
      if ((method != "GET" && method != "HEAD") || !pathOk || hasQuery || !hostOk || malformed) {
        writeStatus(out, 404, "Not Found", emptyMap(), 0)
        return
      }
      forward(method, reqHeaders["range"], out)
    }
  }

  /** Fetches the configured URL (never one derived from the request) and relays status, headers and body. */
  private fun forward(method: String, range: String?, out: OutputStream) {
    val h = headers.toMutableMap()
    if (range != null) h["Range"] = range
    // Everything up to the response headers fails as 502, a pin mismatch included: nothing has been written yet.
    val response = try {
      PinnedHttp.newGetCall(url, h, pin).execute()
    } catch (_: Exception) {
      writeStatus(out, 502, "Bad Gateway", emptyMap(), 0)
      return
    }
    try {
      response.use { r ->
        val relay = LinkedHashMap<String, String>()
        for (name in RELAYED) r.header(name)?.let { relay[name] = it }
        val len = r.header("content-length")?.toLongOrNull()
        writeStatus(out, r.code, r.message.ifEmpty { "OK" }, relay, len)
        if (method == "HEAD") return
        r.body?.byteStream()?.copyTo(out, 64 * 1024)
        out.flush()
      }
    } catch (_: Exception) {
      // Headers are already out: the player reports a broken stream as its own playback error.
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
    proxies.remove(id, this)
    try { server.close() } catch (_: Exception) {}
    pool.shutdownNow()
  }

  companion object {
    private val RELAYED = listOf("content-type", "content-length", "content-range", "accept-ranges")
    /** The only Host values a loopback request can legitimately carry; others hint at DNS rebinding. */
    private val LOOPBACK_HOST = Regex("^(127\\.0\\.0\\.1|localhost)(:\\d{1,5})?$", RegexOption.IGNORE_CASE)
    /** A proxy with no request for this long closes itself. */
    const val IDLE_CLOSE_MS = 600_000L
    private val proxies = ConcurrentHashMap<String, MediaProxy>()
    private val random = SecureRandom()

    /** Starts a proxy for one remote file and returns its loopback URL. */
    fun start(id: String, url: String, headers: Map<String, String>, pin: String?, idleCloseMs: Long = IDLE_CLOSE_MS): String {
      val normalized = PinnedHttp.normalizeFingerprint(pin) ?: throw PinPolicyViolation("missing or invalid certificate pin")
      stop(id)
      val token = ByteArray(24).also { random.nextBytes(it) }
      val p = MediaProxy(
        id,
        ServerSocket(0, 16, InetAddress.getByName("127.0.0.1")),
        url, headers, normalized,
        "/" + token.joinToString("") { "%02x".format(it) },
        idleCloseMs,
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
