package expo.modules.rfetransport

import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import okhttp3.tls.HeldCertificate
import okhttp3.tls.HandshakeCertificates
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Assert.assertThrows
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import javax.net.ssl.SSLHandshakeException
import javax.net.ssl.SSLPeerUnverifiedException

/**
 * Pinning conformance (spike 1A / CI guard): a host whose leaf does not match
 * the saved pin must observe ZERO application bytes.
 */
class PinnedHttpTest {
  private lateinit var server: MockWebServer
  private lateinit var leaf: HeldCertificate
  private lateinit var url: String

  @Before fun setUp() {
    leaf = HeldCertificate.Builder().commonName("rfe-host").addSubjectAlternativeName("localhost").build()
    val certs = HandshakeCertificates.Builder().heldCertificate(leaf).build()
    server = MockWebServer()
    server.useHttps(certs.sslSocketFactory(), false)
    server.start()
    url = server.url("/health").toString()
  }

  @After fun tearDown() { server.shutdown() }

  private fun pin() = PinnedHttp.sha256Hex(leaf.certificate.encoded)

  @Test fun matchingPinSendsRequestAndReturnsBody() {
    server.enqueue(MockResponse().setBody("ok"))
    val r = PinnedHttp.request(url, "GET", mapOf("Authorization" to "Bearer t"), null, pin())
    assertEquals(200, r.status)
    assertEquals("ok", String(r.body))
    assertEquals(pin(), r.seenFingerprint)
    assertEquals("Bearer t", server.takeRequest().getHeader("Authorization"))
  }

  @Test fun colonSeparatedUppercasePinIsNormalized() {
    server.enqueue(MockResponse().setBody("ok"))
    val fp = pin().uppercase().chunked(2).joinToString(":")
    assertEquals(200, PinnedHttp.request(url, "GET", emptyMap(), null, fp).status)
  }

  @Test fun changedPinFailsBeforeAnyApplicationByte() {
    server.enqueue(MockResponse().setBody("secret"))
    val wrong = "0".repeat(64)
    val ex = assertThrows(Exception::class.java) {
      PinnedHttp.request(url, "POST", mapOf("Authorization" to "Bearer secret-token"), "pairing-code".toByteArray(), wrong)
    }
    assertTrue(ex is SSLHandshakeException || ex is SSLPeerUnverifiedException || ex.cause is CertPinMismatch || ex is CertPinMismatch)
    assertEquals("server must see no HTTP request", 0, server.requestCount)
  }

  @Test fun pinnedConnectionIsReusedButNeverServesAnotherPin() {
    server.enqueue(MockResponse().setBody("a"))
    server.enqueue(MockResponse().setBody("b"))
    server.enqueue(MockResponse().setBody("secret"))
    assertEquals(200, PinnedHttp.request(url, "GET", emptyMap(), null, pin()).status)
    assertEquals(200, PinnedHttp.request(url, "GET", emptyMap(), null, pin()).status)
    server.takeRequest()
    assertEquals("second call rides the pooled TLS connection", 1, server.takeRequest().sequenceNumber)
    // A different pin to the same server must handshake afresh and fail, pooled connection or not.
    assertThrows(Exception::class.java) {
      PinnedHttp.request(url, "GET", mapOf("Authorization" to "Bearer t"), null, "1".repeat(64))
    }
    assertEquals(2, server.requestCount)
  }

  @Test fun missingOrInvalidPinIsRefusedWithoutConnecting() {
    val fs = server.url("/v1/fs").toString()
    assertThrows(PinPolicyViolation::class.java) { PinnedHttp.request(fs, "GET", emptyMap(), null, null) }
    assertThrows(PinPolicyViolation::class.java) { PinnedHttp.request(fs, "GET", emptyMap(), null, "abc") }
    assertEquals(0, server.requestCount)
  }

  @Test fun unpinnedPreflightAllowedOnlyForHealthAndChallengeWithoutCredentials() {
    server.enqueue(MockResponse().setBody("{}"))
    server.enqueue(MockResponse().setBody("{}"))
    val base = server.url("/").toString().trimEnd('/')
    assertEquals(200, PinnedHttp.request("$base/v1/health", "GET", emptyMap(), null, null).status)
    assertEquals(200, PinnedHttp.request("$base/v1/auth/challenge", "POST", emptyMap(), null, null).status)
    assertEquals(2, server.requestCount)
    // a bearer token must never ride an unpinned call, even to an allowed path
    assertThrows(PinPolicyViolation::class.java) {
      PinnedHttp.request("$base/v1/health", "GET", mapOf("Authorization" to "Bearer t"), null, null)
    }
    assertThrows(PinPolicyViolation::class.java) { PinnedHttp.request("$base/v1/fs", "GET", emptyMap(), null, null) }
    assertThrows(PinPolicyViolation::class.java) { PinnedHttp.request("$base/v1/pair", "POST", emptyMap(), null, null) }
    assertEquals(2, server.requestCount)
  }

  @Test fun cleartextIsRefused() {
    assertThrows(PinPolicyViolation::class.java) {
      PinnedHttp.request("http://127.0.0.1:1/health", "GET", emptyMap(), null, pin())
    }
  }

  @Test fun probeReturnsFingerprintWithoutSendingHttp() {
    val fp = PinnedHttp.probeFingerprint(url)
    assertEquals(pin(), fp)
    assertEquals(0, server.requestCount)
  }

  @Test fun publicTrustDoesNotBypassPin() {
    // system roots are empty: a wrong pin fails even though TLS itself is fine
    server.enqueue(MockResponse().setBody("x"))
    assertThrows(Exception::class.java) { PinnedHttp.request(url, "GET", emptyMap(), null, "f".repeat(64)) }
    assertEquals(0, server.requestCount)
  }

  @Test fun redirectsAreNotFollowed() {
    server.enqueue(MockResponse().setResponseCode(302).addHeader("Location", "https://evil.example/"))
    val r = PinnedHttp.request(url, "GET", emptyMap(), null, pin())
    assertEquals(302, r.status)
    assertNull(r.headers["x-none"])
    assertNotNull(r.headers["location"])
  }

  @Test fun downloadResumesWithRangeAndFinalizesAtomically() {
    val dest = java.io.File.createTempFile("rfe-dl", ".bin"); dest.delete()
    server.enqueue(MockResponse().setBody("hello world"))
    val n = PinnedHttp.downloadToFile(url, emptyMap(), pin(), dest)
    assertEquals(11L, n)
    assertEquals("hello world", dest.readText())
    assertTrue(!java.io.File(dest.path + ".part").exists())
    dest.delete()
  }

  @Test fun downloadSendsRangeHeaderWhenResuming() {
    val dest = java.io.File.createTempFile("rfe-dl", ".bin"); dest.delete()
    server.enqueue(MockResponse().setResponseCode(206).setBody("world"))
    PinnedHttp.downloadToFile(url, emptyMap(), pin(), dest, offset = 6)
    assertEquals("bytes=6-", server.takeRequest().getHeader("Range"))
    dest.delete()
  }
}

class WolTest {
  @Test fun parsesMacAndBuildsMagicPacket() {
    val mac = Wol.parseMac("01:02:03:04:05:0f")!!
    val p = Wol.packet(mac)
    assertEquals(102, p.size)
    assertEquals(0xFF.toByte(), p[5])
    assertEquals(0x0f.toByte(), p[101])
    assertEquals(null, Wol.parseMac("01:02:03"))
    assertEquals(null, Wol.parseMac("zz:02:03:04:05:06"))
  }
}

class FetchFileTest {
  private lateinit var server: MockWebServer
  private lateinit var leaf: HeldCertificate
  private lateinit var dir: java.io.File

  @Before fun setUp() {
    leaf = HeldCertificate.Builder().commonName("rfe-host").addSubjectAlternativeName("localhost").build()
    server = MockWebServer()
    server.useHttps(HandshakeCertificates.Builder().heldCertificate(leaf).build().sslSocketFactory(), false)
    server.start()
    dir = java.nio.file.Files.createTempDirectory("rfe-fetch").toFile()
  }

  @After fun tearDown() { server.shutdown(); dir.deleteRecursively() }

  /** No leftover `*.part` next to [dest] (partial files are named per fetch id). */
  private fun noPartials(dest: java.io.File) = dest.parentFile?.listFiles()?.none { it.name.endsWith(".part") } ?: true

  private fun pin() = PinnedHttp.sha256Hex(leaf.certificate.encoded)
  private fun url() = server.url("/v1/thumb?path=%2Fa").toString()

  @Test fun publishesFileOnlyOn2xx() {
    server.enqueue(MockResponse().setBody("jpegbytes"))
    val dest = java.io.File(dir, "t/a.jpg")
    val r = FetchFile.get("1", url(), emptyMap(), pin(), dest)
    assertEquals(200, r.status)
    assertEquals("jpegbytes", dest.readText())
    assertTrue(noPartials(dest))
  }

  @Test fun bodyPastCapAbortsAndLeavesNoFile() {
    server.enqueue(MockResponse().setBody("x".repeat(100_000)))
    val dest = java.io.File(dir, "big.bin")
    assertThrows(FetchTooLarge::class.java) { FetchFile.get("c", url(), emptyMap(), pin(), dest, maxBytes = 1000) }
    assertTrue(!dest.exists() && noPartials(dest))
  }

  @Test fun nonSuccessIsAResultAndWritesNothing() {
    server.enqueue(MockResponse().setResponseCode(429).setHeader("Retry-After", "2"))
    val dest = java.io.File(dir, "b.jpg")
    val r = FetchFile.get("2", url(), emptyMap(), pin(), dest)
    assertEquals(429, r.status)
    assertEquals(2, r.retryAfterSeconds)
    assertTrue(!dest.exists())
  }

  @Test fun wrongPinFailsWithNoRequest() {
    server.enqueue(MockResponse().setBody("x"))
    assertThrows(Exception::class.java) { FetchFile.get("3", url(), emptyMap(), "0".repeat(64), java.io.File(dir, "c.jpg")) }
    assertEquals(0, server.requestCount)
  }

  @Test fun cancelAbortsAnInFlightFetchAndLeavesNoFile() {
    server.enqueue(MockResponse().setBody(okio.Buffer().write(ByteArray(500_000))).throttleBody(1000, 100, java.util.concurrent.TimeUnit.MILLISECONDS))
    val dest = java.io.File(dir, "d.jpg")
    var error: Throwable? = null
    val t = Thread { try { FetchFile.get("4", url(), emptyMap(), pin(), dest) } catch (e: Throwable) { error = e } }
    t.start()
    Thread.sleep(400)
    FetchFile.cancel("4")
    t.join(5000)
    assertNotNull(error)
    assertTrue(!dest.exists() && noPartials(dest))
  }

  @Test fun twoFetchesOfOneDestinationEachPublishACompleteFile() {
    val body = "z".repeat(200_000)
    repeat(2) { server.enqueue(MockResponse().setBody(body).throttleBody(50_000, 100, java.util.concurrent.TimeUnit.MILLISECONDS)) }
    val dest = java.io.File(dir, "same.jpg")
    val errors = java.util.Collections.synchronizedList(mutableListOf<Throwable>())
    val threads = listOf("a", "b").map { id -> Thread { try { FetchFile.get(id, url(), emptyMap(), pin(), dest) } catch (e: Throwable) { errors += e } } }
    threads.forEach { it.start() }
    threads.forEach { it.join(10000) }
    assertTrue(errors.toString(), errors.isEmpty())
    assertEquals(body, dest.readText())
    assertTrue(noPartials(dest))
  }
}
