package expo.modules.rfetransport

import okhttp3.Headers.Companion.toHeaders
import okhttp3.HttpUrl.Companion.toHttpUrl
import okhttp3.MediaType.Companion.toMediaTypeOrNull
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.RequestBody.Companion.toRequestBody
import java.io.File
import java.io.FileOutputStream
import java.security.MessageDigest
import java.security.SecureRandom
import java.security.cert.CertificateException
import java.security.cert.X509Certificate
import java.util.concurrent.TimeUnit
import javax.net.ssl.SSLContext
import javax.net.ssl.X509TrustManager

/** Thrown when the presented leaf certificate does not match the saved pin. */
class CertPinMismatch(val pinned: String, val seen: String) :
  CertificateException("Certificate fingerprint mismatch (pinned=$pinned seen=$seen)")

/** Thrown when a request is attempted without a valid pin or over cleartext. */
class PinPolicyViolation(message: String) : IllegalStateException(message)

class PinnedResponse(
  val status: Int,
  val headers: Map<String, String>,
  val body: ByteArray,
  val seenFingerprint: String?,
)

/**
 * Pinned HTTPS transport, mirroring Flutter's agent_client.dart: an empty trust
 * store (no system roots), and the exact leaf-certificate SHA-256 compared
 * during the TLS handshake, before OkHttp writes any request line, header or
 * body. Hostname is intentionally not verified: the pin is the host identity
 * (one pin per host across LAN, Tailscale and direct HTTPS routes).
 */
object PinnedHttp {
  private val fpRegex = Regex("^[0-9a-f]{64}$")

  fun normalizeFingerprint(fp: String?): String? {
    val n = fp?.trim()?.replace(":", "")?.lowercase() ?: return null
    return if (fpRegex.matches(n)) n else null
  }

  fun sha256Hex(der: ByteArray): String =
    MessageDigest.getInstance("SHA-256").digest(der).joinToString("") { "%02x".format(it) }

  private class PinTrustManager(private val pinned: String?) : X509TrustManager {
    @Volatile var seen: String? = null

    override fun checkClientTrusted(chain: Array<out X509Certificate>?, authType: String?) {
      throw CertificateException("client certificates are not used")
    }

    override fun checkServerTrusted(chain: Array<out X509Certificate>?, authType: String?) {
      val leaf = chain?.firstOrNull() ?: throw CertificateException("empty certificate chain")
      val fp = sha256Hex(leaf.encoded)
      seen = fp
      // pinned == null is the fingerprint-discovery probe only: the handshake
      // completes but request() below never sends an application byte for it.
      if (pinned != null && fp != pinned) throw CertPinMismatch(pinned, fp)
    }

    override fun getAcceptedIssuers(): Array<X509Certificate> = emptyArray()
  }

  // One client per pin, so pinned calls reuse TLS connections (and HTTP/2) like Flutter's HttpClient.
  // OkHttp keys pooled connections by the SSL socket factory, which is per pin, so a connection
  // verified under one pin is never reused for another.
  private val pinned = java.util.concurrent.ConcurrentHashMap<String, OkHttpClient>()

  private fun pinnedClient(pin: String, timeoutMs: Long): OkHttpClient {
    val base = pinned.getOrPut(pin) { client(PinTrustManager(pin), 30000) }
    return base.newBuilder()
      .connectTimeout(timeoutMs, TimeUnit.MILLISECONDS)
      .readTimeout(timeoutMs, TimeUnit.MILLISECONDS)
      .writeTimeout(timeoutMs, TimeUnit.MILLISECONDS)
      .build()
  }

  private fun client(tm: PinTrustManager, timeoutMs: Long): OkHttpClient {
    val ctx = SSLContext.getInstance("TLS")
    ctx.init(null, arrayOf(tm), SecureRandom())
    return OkHttpClient.Builder()
      .sslSocketFactory(ctx.socketFactory, tm)
      .hostnameVerifier { _, _ -> true }
      .followRedirects(false)
      .followSslRedirects(false)
      .retryOnConnectionFailure(false)
      .connectTimeout(timeoutMs, TimeUnit.MILLISECONDS)
      .readTimeout(timeoutMs, TimeUnit.MILLISECONDS)
      .writeTimeout(timeoutMs, TimeUnit.MILLISECONDS)
      .build()
  }

  fun isSafeUnpinnedPreflight(method: String, url: String): Boolean {
    val path = url.toHttpUrl().encodedPath
    return (method == "GET" && path == "/v1/health") || (method == "POST" && path == "/v1/auth/challenge")
  }

  private fun requireHttps(url: String) {
    if (!url.toHttpUrl().isHttps) throw PinPolicyViolation("cleartext HTTP is refused")
  }

  /** TLS handshake only; returns the leaf SHA-256 without sending any HTTP bytes. */
  fun probeFingerprint(url: String, timeoutMs: Long = 8000): String {
    requireHttps(url)
    val tm = PinTrustManager(null)
    val http = client(tm, timeoutMs)
    val u = url.toHttpUrl()
    // Connect with a deadline first: createSocket(host, port) connects with none, so an address that drops
    // SYNs would hold the probe for minutes (audit F-152).
    val raw = java.net.Socket()
    val sock: javax.net.ssl.SSLSocket
    try {
      raw.connect(java.net.InetSocketAddress(u.host, u.port), timeoutMs.toInt())
      sock = http.sslSocketFactory.createSocket(raw, u.host, u.port, true) as javax.net.ssl.SSLSocket
    } catch (e: Exception) {
      raw.close()
      throw e
    }
    try {
      sock.soTimeout = timeoutMs.toInt()
      sock.startHandshake()
    } finally {
      sock.close()
    }
    return tm.seen ?: throw CertificateException("no certificate presented")
  }

  fun request(
    url: String,
    method: String,
    headers: Map<String, String>,
    body: ByteArray?,
    pin: String?,
    timeoutMs: Long = 30000,
  ): PinnedResponse {
    requireHttps(url)
    val normalized = normalizeFingerprint(pin)
    if (normalized == null) {
      // Mirrors agent_client.dart: without a pin only GET /v1/health and
      // POST /v1/auth/challenge may run, and never with credentials.
      if (!isSafeUnpinnedPreflight(method, url) || headers.keys.any { it.equals("authorization", true) }) {
        throw PinPolicyViolation("missing or invalid certificate pin")
      }
    }
    val req = Request.Builder().url(url).headers(headers.toHeaders())
      .method(method, body?.toRequestBody("application/octet-stream".toMediaTypeOrNull())
        ?: if (method == "POST" || method == "PUT" || method == "PATCH") ByteArray(0).toRequestBody(null) else null)
      .build()
    // Unpinned preflights get a fresh client so the fingerprint seen on this handshake is reported.
    val tm = if (normalized == null) PinTrustManager(null) else null
    val http = if (tm != null) client(tm, timeoutMs) else pinnedClient(normalized!!, timeoutMs)
    http.newCall(req).execute().use { r ->
      val h = HashMap<String, String>()
      for (i in 0 until r.headers.size) h[r.headers.name(i).lowercase()] = r.headers.value(i)
      // A pinned response only exists if the handshake matched the pin.
      return PinnedResponse(r.code, h, readCapped(r), tm?.seen ?: normalized)
    }
  }

  /** The biggest response read into memory by [request]; file bodies stream through [newGetCall] and [newCall] instead (audit F-155). */
  const val MAX_BODY_BYTES = 64L * 1024 * 1024

  private fun readCapped(r: okhttp3.Response): ByteArray {
    val body = r.body ?: return ByteArray(0)
    val source = body.source()
    source.request(MAX_BODY_BYTES + 1)
    if (source.buffer.size > MAX_BODY_BYTES) throw java.io.IOException("the response is larger than ${MAX_BODY_BYTES / (1024 * 1024)} MiB")
    return source.readByteArray()
  }

  /** Pinned GET call for streaming; the caller executes it and may cancel it. */
  fun newGetCall(url: String, headers: Map<String, String>, pin: String?, timeoutMs: Long = 30000): okhttp3.Call {
    requireHttps(url)
    val normalized = normalizeFingerprint(pin) ?: throw PinPolicyViolation("missing or invalid certificate pin")
    val req = Request.Builder().url(url).headers(headers.toHeaders()).get().build()
    return pinnedClient(normalized, timeoutMs).newCall(req)
  }

  /** Pinned call with an optional request body, for streaming or cancellable requests; refuses without a pin. */
  fun newCall(method: String, url: String, headers: Map<String, String>, body: okhttp3.RequestBody?, pin: String?, timeoutMs: Long = 30000): okhttp3.Call {
    requireHttps(url)
    val normalized = normalizeFingerprint(pin) ?: throw PinPolicyViolation("missing or invalid certificate pin")
    val needsBody = method == "POST" || method == "PUT" || method == "PATCH"
    val req = Request.Builder().url(url).headers(headers.toHeaders())
      .method(method, body ?: if (needsBody) ByteArray(0).toRequestBody(null) else null).build()
    return pinnedClient(normalized, timeoutMs).newCall(req)
  }

  /**
   * Streams the response to [dest] in a temp file, appending from [offset] when
   * the server honours the Range request; renames atomically on success.
   */
  fun downloadToFile(
    url: String,
    headers: Map<String, String>,
    pin: String?,
    dest: File,
    offset: Long = 0,
    timeoutMs: Long = 30000,
  ): Long {
    requireHttps(url)
    val normalized = normalizeFingerprint(pin) ?: throw PinPolicyViolation("missing or invalid certificate pin")
    val h = headers.toMutableMap()
    if (offset > 0) h["Range"] = "bytes=$offset-"
    val req = Request.Builder().url(url).headers(h.toHeaders()).get().build()
    val part = File(dest.path + ".part")
    pinnedClient(normalized, timeoutMs).newCall(req).execute().use { r ->
      if (!r.isSuccessful) throw java.io.IOException("HTTP ${r.code}")
      val resume = offset > 0 && r.code == 206
      FileOutputStream(part, resume).use { out ->
        r.body!!.byteStream().copyTo(out, 64 * 1024)
      }
    }
    if (!part.renameTo(dest)) throw java.io.IOException("could not finalize download")
    return dest.length()
  }
}
