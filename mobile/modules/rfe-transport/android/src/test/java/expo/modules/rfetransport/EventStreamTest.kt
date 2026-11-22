package expo.modules.rfetransport

import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import okhttp3.tls.HandshakeCertificates
import okhttp3.tls.HeldCertificate
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Before
import org.junit.Test

class EventStreamTest {
  private lateinit var server: MockWebServer
  private lateinit var leaf: HeldCertificate

  @Before fun setUp() {
    leaf = HeldCertificate.Builder().commonName("rfe-host").addSubjectAlternativeName("localhost").build()
    server = MockWebServer()
    server.useHttps(HandshakeCertificates.Builder().heldCertificate(leaf).build().sslSocketFactory(), false)
    server.start()
  }

  @After fun tearDown() { server.shutdown() }

  private fun pin() = PinnedHttp.sha256Hex(leaf.certificate.encoded)
  private fun url() = server.url("/v1/events").toString()
  private fun sse(vararg kinds: String) =
    MockResponse().setHeader("Content-Type", "text/event-stream").setBody(kinds.joinToString("") { "event: $it\n\n" })

  @Test
  fun yieldsOneKindPerEventAndSkipsPings() {
    val got = mutableListOf<String>()
    val lines = listOf(": ping", "", "id: 1", "event: files\r", "data: {\"kind\":\"files\",\"seq\":1}", "", "id: 2", "event: transfers", "")
    EventStream.parse(lines.asSequence()) { got.add(it) }
    assertEquals(listOf("files", "transfers"), got)
  }

  @Test
  fun anUnfinishedEventIsDropped() {
    val got = mutableListOf<String>()
    EventStream.parse(sequenceOf("event: files")) { got.add(it) }
    assertEquals(emptyList<String>(), got)
  }

  /** The JS side stops reconnecting on -1 and shows the identity-changed message; 0 would mean "offline, retry". */
  @Test
  fun aCertificateThatDoesNotMatchThePinIsReportedAsMinusOneNotAsAnOfflineHost() {
    server.enqueue(sse("files"))
    val got = mutableListOf<String>()
    val status = EventStream.run("pin1", url(), mapOf("Authorization" to "Bearer secret-token"), "0".repeat(64)) { got.add(it) }
    assertEquals(-1, status)
    assertEquals(emptyList<String>(), got)
    assertEquals("the handshake must fail before any HTTP byte, so no bearer token is sent", 0, server.requestCount)
  }

  @Test
  fun anUnreachableHostStaysZeroSoTheBackoffReconnectKeepsWorking() {
    val dead = server.url("/v1/events").toString()
    server.shutdown()
    assertEquals(0, EventStream.run("down1", dead, emptyMap(), pin()) { })
  }

  /** stop() of an id with no running stream must not cancel a later run of the same id. */
  @Test
  fun stopAfterAStreamEndedDoesNotCancelTheNextRun() {
    server.enqueue(sse("files"))
    server.enqueue(sse("transfers"))
    val first = mutableListOf<String>()
    assertEquals(200, EventStream.run("again", url(), emptyMap(), pin()) { first.add(it) })
    EventStream.stop("again")
    val second = mutableListOf<String>()
    assertEquals(200, EventStream.run("again", url(), emptyMap(), pin()) { second.add(it) })
    assertEquals(listOf("files"), first)
    assertEquals(listOf("transfers"), second)
  }

  @Test
  fun stopCancelsAStreamThatIsRunning() {
    server.enqueue(MockResponse().setHeader("Content-Type", "text/event-stream").setBody("event: hello\n\n").setSocketPolicy(okhttp3.mockwebserver.SocketPolicy.NO_RESPONSE))
    val status = java.util.concurrent.atomic.AtomicInteger(-99)
    val t = Thread { status.set(EventStream.run("live", url(), emptyMap(), pin()) { }) }
    t.start()
    val deadline = System.nanoTime() + 5_000_000_000L
    while (server.requestCount == 0 && System.nanoTime() < deadline) Thread.sleep(10)
    EventStream.stop("live")
    t.join(5_000)
    assertEquals("stopped stream returns 0 (connection ended)", 0, status.get())
  }
}
