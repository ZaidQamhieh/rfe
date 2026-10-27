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

  @Test fun refusesToStartWithoutAPin() {
    val ex = runCatching { MediaProxy.start("x", agent.url("/").toString(), emptyMap(), null) }.exceptionOrNull()
    assertTrue(ex is PinPolicyViolation)
  }
}
