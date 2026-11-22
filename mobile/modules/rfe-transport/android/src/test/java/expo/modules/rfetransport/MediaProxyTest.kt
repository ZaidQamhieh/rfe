package expo.modules.rfetransport

import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import okhttp3.tls.HandshakeCertificates
import okhttp3.tls.HeldCertificate
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import java.net.HttpURLConnection
import java.net.Socket
import java.net.URL

/** Port of video_loopback_proxy_test.dart plus the forward path (Range, auth, relay). */
class MediaProxyTest {
  private lateinit var agent: MockWebServer
  private lateinit var leaf: HeldCertificate
  private lateinit var proxyUrl: String

  @Before fun setUp() {
    leaf = HeldCertificate.Builder().commonName("rfe-host").addSubjectAlternativeName("localhost").build()
    agent = MockWebServer()
    agent.useHttps(HandshakeCertificates.Builder().heldCertificate(leaf).build().sslSocketFactory(), false)
    agent.start()
    val pin = PinnedHttp.sha256Hex(leaf.certificate.encoded)
    proxyUrl = MediaProxy.start("t", agent.url("/v1/content?path=%2Fv.mp4").toString(), mapOf("Authorization" to "Bearer tok"), pin)
  }

  @After fun tearDown() {
    MediaProxy.stop("t")
    agent.shutdown()
  }

  private fun open(method: String, url: String, range: String? = null): HttpURLConnection =
    (URL(url).openConnection() as HttpURLConnection).apply {
      requestMethod = method
      range?.let { setRequestProperty("Range", it) }
    }

  @Test fun pathIsRandomAndLoopbackOnly() {
    val u = URL(proxyUrl)
    assertEquals("127.0.0.1", u.host)
    assertTrue(u.path.length > 40)
  }

  @Test fun wrongPathOrMethodNeverReachesTheAgent() {
    val base = proxyUrl.substringBeforeLast('/')
    assertEquals(404, open("GET", "$base/video").responseCode)
    assertEquals(404, open("GET", "$base/").responseCode)
    assertEquals(404, open("POST", proxyUrl).responseCode)
    assertEquals(404, open("DELETE", proxyUrl).responseCode)
    assertEquals(0, agent.requestCount)
  }

  @Test fun forwardsRangeAndAuthAndRelaysPartialContent() {
    agent.enqueue(MockResponse().setResponseCode(206).setHeader("Content-Type", "video/mp4").setHeader("Content-Range", "bytes 2-5/10").setHeader("Accept-Ranges", "bytes").setBody("2345"))
    val c = open("GET", proxyUrl, "bytes=2-5")
    assertEquals(206, c.responseCode)
    assertEquals("video/mp4", c.getHeaderField("Content-Type"))
    assertEquals("bytes 2-5/10", c.getHeaderField("Content-Range"))
    assertEquals("2345", c.inputStream.readBytes().toString(Charsets.UTF_8))
    val seen = agent.takeRequest()
    assertEquals("bytes=2-5", seen.getHeader("Range"))
    assertEquals("Bearer tok", seen.getHeader("Authorization"))
  }

  /** Sends a raw request line, so targets that URL/HttpURLConnection would normalise reach the proxy unchanged. */
  private fun rawStatus(target: String): Int {
    Socket("127.0.0.1", URL(proxyUrl).port).use { s ->
      s.soTimeout = 5_000
      s.getOutputStream().write("GET $target HTTP/1.1\r\nHost: 127.0.0.1\r\n\r\n".toByteArray(Charsets.ISO_8859_1))
      s.getOutputStream().flush()
      val line = checkNotNull(s.getInputStream().bufferedReader(Charsets.ISO_8859_1).readLine())
      return line.split(' ')[1].toInt()
    }
  }

  @Test fun noQueryForwardsTheConfiguredUrlUnchanged() {
    agent.enqueue(MockResponse().setResponseCode(200).setBody("whole"))
    val c = open("GET", proxyUrl)
    assertEquals(200, c.responseCode)
    assertEquals("whole", c.inputStream.readBytes().toString(Charsets.UTF_8))
    assertEquals("/v1/content?path=%2Fv.mp4", agent.takeRequest().path)
  }

  /** The agent's file endpoint takes ?path=, so a player-supplied query would let a token holder read any file with the bearer token. */
  @Test fun aPlayerQueryIsRejectedAndNeverReachesTheAgent() {
    val tok = URL(proxyUrl).path
    assertEquals(404, rawStatus("$tok?path=%2Fetc%2Fsecret"))
    assertEquals(404, open("GET", "$proxyUrl?quality=720&seg=3").responseCode)
    assertEquals(404, rawStatus("$tok?"))
    assertEquals(0, agent.requestCount)
  }

  @Test fun headStillWorksAndReachesTheConfiguredUrl() {
    agent.enqueue(MockResponse().setResponseCode(200).setHeader("Content-Type", "video/mp4").setHeader("Content-Length", "10"))
    val c = open("HEAD", proxyUrl)
    assertEquals(200, c.responseCode)
    assertEquals("video/mp4", c.getHeaderField("Content-Type"))
    assertEquals("/v1/content?path=%2Fv.mp4", agent.takeRequest().path)
  }

  private fun rawStatusWithHost(host: String?): Int {
    Socket("127.0.0.1", URL(proxyUrl).port).use { s ->
      s.soTimeout = 5_000
      val hostLine = if (host == null) "" else "Host: $host\r\n"
      s.getOutputStream().write("GET ${URL(proxyUrl).path} HTTP/1.1\r\n$hostLine\r\n".toByteArray(Charsets.ISO_8859_1))
      s.getOutputStream().flush()
      return checkNotNull(s.getInputStream().bufferedReader(Charsets.ISO_8859_1).readLine()).split(' ')[1].toInt()
    }
  }

  /** DNS-rebinding hygiene: a request addressed to any other name is not for this proxy. */
  @Test fun aForeignHostHeaderIsRejected() {
    assertEquals(404, rawStatusWithHost("evil.example"))
    assertEquals(404, rawStatusWithHost("evil.example:${URL(proxyUrl).port}"))
    assertEquals(404, rawStatusWithHost("127.0.0.1.evil.example"))
    assertEquals(0, agent.requestCount)
  }

  @Test fun loopbackHostHeadersAreAccepted() {
    for (h in listOf("127.0.0.1", "127.0.0.1:${URL(proxyUrl).port}", "localhost:${URL(proxyUrl).port}")) {
      agent.enqueue(MockResponse().setResponseCode(200).setBody("ok"))
      assertEquals(h, 200, rawStatusWithHost(h))
    }
  }

  /** A pin failure must answer 502 before any header, not close the socket silently. */
  @Test fun aPinMismatchOnTheAgentAnswers502AndSendsNoRequest() {
    MediaProxy.stop("t")
    val wrongPin = "0".repeat(64)
    val u = MediaProxy.start("t", agent.url("/v1/content?path=%2Fv.mp4").toString(), mapOf("Authorization" to "Bearer tok"), wrongPin)
    assertEquals(502, open("GET", u).responseCode)
    assertEquals(0, agent.requestCount)
  }

  @Test fun anIdleProxyClosesItself() {
    MediaProxy.stop("t")
    val u = MediaProxy.start("idle", agent.url("/v1/content?path=%2Fv.mp4").toString(), emptyMap(), PinnedHttp.sha256Hex(leaf.certificate.encoded), idleCloseMs = 150)
    val port = URL(u).port
    // Each probe is itself a connection and counts as activity, so probe far less often than the idle period.
    val deadline = System.nanoTime() + 5_000_000_000L
    var refused = false
    while (!refused && System.nanoTime() < deadline) {
      Thread.sleep(400)
      refused = try { Socket("127.0.0.1", port).close(); false } catch (_: java.io.IOException) { true }
    }
    assertTrue("proxy should stop listening after the idle period", refused)
    MediaProxy.stop("idle")
  }

  @Test fun wrongPathWithQueryIsNotForwarded() {
    val tok = URL(proxyUrl).path
    assertEquals(404, rawStatus("/video?seg=3"))
    assertEquals(404, rawStatus(tok + "/extra?seg=3"))
    assertEquals(0, agent.requestCount)
  }

  @Test fun fragmentOrWhitespaceInTargetIsRejected() {
    val tok = URL(proxyUrl).path
    assertEquals(404, rawStatus(tok + "?seg=3#frag"))
    assertEquals(404, rawStatus(tok + "?seg=3 x"))
    assertEquals(0, agent.requestCount)
  }

  @Test fun refusesToStartWithoutAPin() {
    val ex = runCatching { MediaProxy.start("x", agent.url("/").toString(), emptyMap(), null) }.exceptionOrNull()
    assertTrue(ex is PinPolicyViolation)
  }
}
