package expo.modules.rfetransport

import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import okhttp3.mockwebserver.SocketPolicy
import okhttp3.tls.HandshakeCertificates
import okhttp3.tls.HeldCertificate
import okio.Buffer
import org.junit.After
import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import org.json.JSONObject
import java.io.File
import java.nio.file.Files
import java.util.concurrent.TimeUnit

class TransferEngineTest {
  private lateinit var server: MockWebServer
  private lateinit var leaf: HeldCertificate
  private lateinit var dir: File
  private lateinit var engine: TransferEngine
  private val payload = ByteArray(300_000) { (it * 31 % 251).toByte() }
  private var pin: String? = null

  private val creds = object : Credentials {
    override fun pin(hostId: String) = pin
    override fun token(hostId: String) = "tok"
  }

  @Before fun setUp() {
    leaf = HeldCertificate.Builder().commonName("rfe-host").addSubjectAlternativeName("localhost").build()
    val certs = HandshakeCertificates.Builder().heldCertificate(leaf).build()
    server = MockWebServer()
    server.useHttps(certs.sslSocketFactory(), false)
    server.start()
    pin = PinnedHttp.sha256Hex(leaf.certificate.encoded)
    dir = Files.createTempDirectory("rfe-engine").toFile()
    engine = TransferEngine(File(dir, "journal"), creds, timeoutMs = 5000)
  }

  @After fun tearDown() {
    engine.shutdown()
    server.shutdown()
    dir.deleteRecursively()
  }

  private val address get() = "${server.hostName}:${server.port}"
  private fun dest(name: String) = File(dir, "dl/$name").path

  private fun await(id: String, vararg states: TransferState, ms: Long = 10_000): TransferRecord {
    val end = System.currentTimeMillis() + ms
    while (System.currentTimeMillis() < end) {
      engine.list().firstOrNull { it.id == id && it.state in states }?.let { return it }
      Thread.sleep(20)
    }
    throw AssertionError("timeout waiting for $states, have ${engine.list()}")
  }

  private val lastModified = "Wed, 21 Oct 2026 07:28:00 GMT"
  private fun full() = MockResponse().setBody(Buffer().write(payload)).setHeader("Last-Modified", lastModified)

  @Test fun downloadsAndFinalizesAtomically() {
    server.enqueue(full())
    engine.enqueue("t1", "h", address, "/a.bin", dest("a.bin"))
    val r = await("t1", TransferState.DONE)
    assertArrayEquals(payload, File(dest("a.bin")).readBytes())
    assertFalse(File(dest("a.bin") + ".part").exists())
    assertEquals(payload.size.toLong(), r.total)
    val req = server.takeRequest()
    assertEquals("Bearer tok", req.getHeader("Authorization"))
    assertTrue(req.path!!.startsWith("/v1/content?path=%2Fa.bin"))
    assertEquals(null, req.getHeader("Range"))
  }

  @Test fun refusedDownloadFailsWithTheAgentCodeNotAConnectionError() {
    server.enqueue(MockResponse().setResponseCode(403).setBody("{\"code\":\"CAPABILITY_DENIED\",\"message\":\"device lacks download permission\"}"))
    engine.enqueue("t0", "h", address, "/a.bin", dest("a.bin"))
    val r = await("t0", TransferState.FAILED)
    assertEquals("CAPABILITY_DENIED", r.error)
    assertFalse(File(dest("a.bin")).exists())
  }

  @Test fun interruptedTransferResumesWithRangeFromPartialLength() {
    server.enqueue(full().setSocketPolicy(SocketPolicy.DISCONNECT_DURING_RESPONSE_BODY))
    engine.enqueue("t2", "h", address, "/b.bin", dest("b.bin"))
    val failed = await("t2", TransferState.FAILED)
    val partLen = File(dest("b.bin") + ".part").length()
    assertTrue("partial written ($partLen)", partLen in 1 until payload.size)
    assertTrue(failed.error!!.startsWith("ERR_CONNECTION"))
    server.takeRequest()

    server.enqueue(
      MockResponse().setResponseCode(206).setBody(Buffer().write(payload, partLen.toInt(), payload.size - partLen.toInt())),
    )
    engine.resume("t2")
    await("t2", TransferState.DONE)
    val resumeReq = server.takeRequest()
    assertEquals("bytes=$partLen-", resumeReq.getHeader("Range"))
    assertEquals(lastModified, resumeReq.getHeader("If-Range"))
    assertArrayEquals(payload, File(dest("b.bin")).readBytes())
  }

  @Test fun resumeAfterTheFileChangedRestartsFromZeroInsteadOfAppending() {
    server.enqueue(full().setSocketPolicy(SocketPolicy.DISCONNECT_DURING_RESPONSE_BODY))
    engine.enqueue("t2b", "h", address, "/b.bin", dest("b.bin"))
    await("t2b", TransferState.FAILED)
    assertTrue(File(dest("b.bin") + ".part").length() > 0)
    server.takeRequest()

    // If-Range no longer matches, so a real server answers 200 with the whole new file.
    val changed = ByteArray(payload.size + 77) { (it * 7 % 253).toByte() }
    server.enqueue(MockResponse().setBody(Buffer().write(changed)).setHeader("Last-Modified", "Thu, 22 Oct 2026 07:28:00 GMT"))
    engine.resume("t2b")
    await("t2b", TransferState.DONE)
    assertEquals(lastModified, server.takeRequest().getHeader("If-Range"))
    assertArrayEquals(changed, File(dest("b.bin")).readBytes())
  }

  @Test fun partialWithoutAKnownVersionIsNotResumed() {
    File(dest("v.bin") + ".part").apply { parentFile.mkdirs(); writeBytes(ByteArray(1000) { 9 }) }
    server.enqueue(full())
    engine.enqueue("t2c", "h", address, "/v.bin", dest("v.bin"))
    await("t2c", TransferState.DONE)
    val req = server.takeRequest()
    assertEquals(null, req.getHeader("Range"))
    assertArrayEquals(payload, File(dest("v.bin")).readBytes())
  }

  @Test fun serverIgnoringRangeRestartsFromZeroWithoutCorruption() {
    File(dest("c.bin") + ".part").apply { parentFile.mkdirs(); writeBytes(ByteArray(1000) { 9 }) }
    server.enqueue(full()) // 200, not 206
    engine.enqueue("t3", "h", address, "/c.bin", dest("c.bin"))
    await("t3", TransferState.DONE)
    assertArrayEquals(payload, File(dest("c.bin")).readBytes())
  }

  @Test fun pauseKeepsPartialAndResumeCompletes() {
    server.enqueue(full().throttleBody(20_000, 100, TimeUnit.MILLISECONDS))
    engine.enqueue("t4", "h", address, "/d.bin", dest("d.bin"))
    Thread.sleep(400)
    engine.pause("t4")
    await("t4", TransferState.PAUSED)
    val partLen = File(dest("d.bin") + ".part").length()
    assertTrue(partLen in 1 until payload.size)
    server.takeRequest()
    server.enqueue(MockResponse().setResponseCode(206).setBody(Buffer().write(payload, partLen.toInt(), payload.size - partLen.toInt())))
    engine.resume("t4")
    await("t4", TransferState.DONE)
    assertEquals("bytes=$partLen-", server.takeRequest().getHeader("Range"))
    assertArrayEquals(payload, File(dest("d.bin")).readBytes())
  }

  @Test fun cancelRemovesPartial() {
    server.enqueue(full().throttleBody(10_000, 100, TimeUnit.MILLISECONDS))
    engine.enqueue("t5", "h", address, "/e.bin", dest("e.bin"))
    Thread.sleep(300)
    engine.cancel("t5")
    await("t5", TransferState.CANCELLED)
    Thread.sleep(200)
    assertFalse(File(dest("e.bin") + ".part").exists())
    assertFalse(File(dest("e.bin")).exists())
  }

  @Test fun wrongPinFailsClosedWithNoRequestAndNoFile() {
    pin = "0".repeat(64)
    server.enqueue(full())
    engine.enqueue("t6", "h", address, "/f.bin", dest("f.bin"))
    val r = await("t6", TransferState.FAILED)
    assertEquals("ERR_CERT_PIN_MISMATCH", r.error)
    assertEquals(0, server.requestCount)
    assertFalse(File(dest("f.bin") + ".part").exists())
  }

  @Test fun missingPinIsRefusedWithNoRequest() {
    pin = null
    server.enqueue(full())
    engine.enqueue("t7", "h", address, "/g.bin", dest("g.bin"))
    assertEquals("ERR_PIN_POLICY", await("t7", TransferState.FAILED).error)
    assertEquals(0, server.requestCount)
  }

  @Test fun journalSurvivesRestartAndRunningBecomesPaused() {
    server.enqueue(full().throttleBody(5_000, 200, TimeUnit.MILLISECONDS))
    engine.enqueue("t10", "h", address, "/j.bin", dest("j.bin"))
    Thread.sleep(300)
    engine.shutdown() // simulates process death: journal says RUNNING
    val reborn = TransferEngine(File(dir, "journal"), creds, timeoutMs = 5000)
    val rec = reborn.list().first { it.id == "t10" }
    assertEquals(TransferState.PAUSED, rec.state)
    assertNotNull(File(dest("j.bin") + ".part").takeIf { it.exists() })
    reborn.shutdown()
  }

  @Test fun rejectsUnsafeTransferIds() {
    org.junit.Assert.assertThrows(IllegalArgumentException::class.java) {
      engine.enqueue("../evil", "h", address, "/x", dest("x"))
    }
  }

  @Test fun stampsTheTimeAFinishedDownloadCompleted() {
    val start = System.currentTimeMillis()
    val eng = TransferEngine(File(dir, "journal-time"), creds, timeoutMs = 5000)
    try {
      server.enqueue(full())
      eng.enqueue("t1", "h", address, "/a.bin", dest("t1.bin"))
      val end = System.currentTimeMillis() + 10_000
      while (eng.list().none { it.id == "t1" && it.state == TransferState.DONE } && System.currentTimeMillis() < end) Thread.sleep(20)
      val r = eng.list().first { it.id == "t1" }
      assertTrue("updatedAt ${r.updatedAt} should be at or after the start $start", r.updatedAt >= start)
      assertEquals(r.updatedAt, TransferRecord.fromJson(r.toJson()).updatedAt)
      assertEquals(0L, TransferRecord.fromJson(JSONObject(r.toJson().toString()).apply { remove("updatedAt") }).updatedAt)
    } finally {
      eng.shutdown()
    }
  }

  @Test fun publishesFinishedDownloadsAndDropsThePrivateCopy() {
    val published = mutableListOf<ByteArray>()
    val pub = TransferEngine(File(dir, "journal-pub"), creds, timeoutMs = 5000, publisher = { f -> published.add(f.readBytes()); "content://media/external/downloads/42" })
    try {
      server.enqueue(full())
      pub.enqueue("p1", "h", address, "/a.bin", dest("p1.bin"))
      val end = System.currentTimeMillis() + 10_000
      while (pub.list().none { it.id == "p1" && it.state == TransferState.DONE } && System.currentTimeMillis() < end) Thread.sleep(20)
      val r = pub.list().first { it.id == "p1" }
      assertEquals(TransferState.DONE, r.state)
      assertEquals("content://media/external/downloads/42", r.publicUri)
      assertEquals(payload.size.toLong(), r.received)
      assertArrayEquals(payload, published.single())
      assertFalse(File(dest("p1.bin")).exists())
    } finally {
      pub.shutdown()
    }
  }

  @Test fun removesThePerTransferStagingFolderOncePublished() {
    val pub = TransferEngine(File(dir, "journal-pub3"), creds, timeoutMs = 5000, publisher = { "content://media/external/downloads/7" })
    try {
      server.enqueue(full())
      val folder = File(dir, "p3").also { it.mkdirs() }
      pub.enqueue("p3", "h", address, "/a.bin", File(folder, "a.bin").path)
      val end = System.currentTimeMillis() + 10_000
      while (pub.list().none { it.id == "p3" && it.state == TransferState.DONE } && System.currentTimeMillis() < end) Thread.sleep(20)
      assertEquals(TransferState.DONE, pub.list().first { it.id == "p3" }.state)
      assertFalse(folder.exists())
    } finally {
      pub.shutdown()
    }
  }

  @Test fun keepsTheFileInAppStorageWhenPublishingFails() {
    val pub = TransferEngine(File(dir, "journal-pub2"), creds, timeoutMs = 5000, publisher = { null })
    try {
      server.enqueue(full())
      pub.enqueue("p2", "h", address, "/a.bin", dest("p2.bin"))
      val end = System.currentTimeMillis() + 10_000
      while (pub.list().none { it.id == "p2" && it.state == TransferState.DONE } && System.currentTimeMillis() < end) Thread.sleep(20)
      assertEquals(null, pub.list().first { it.id == "p2" }.publicUri)
      assertArrayEquals(payload, File(dest("p2.bin")).readBytes())
    } finally {
      pub.shutdown()
    }
  }

  @Test fun fullDiskFailsWithItsOwnCodeNotAConnectionError() {
    org.junit.Assume.assumeTrue(File("/dev/full").exists())
    server.enqueue(full())
    val d = File(dest("full.bin")).also { it.parentFile!!.mkdirs() }
    Files.createSymbolicLink(File(d.path + ".part").toPath(), File("/dev/full").toPath())
    engine.enqueue("t-full", "h", address, "/full.bin", d.path)
    val r = await("t-full", TransferState.FAILED)
    assertEquals("ERR_STORAGE_FULL", r.error)
  }

  @Test fun failureReachesTheListenerEvenWhenTheJournalCannotBeWritten() {
    val seen = java.util.concurrent.CopyOnWriteArrayList<TransferRecord>()
    val journal = File(dir, "journal2")
    val e = TransferEngine(journal, creds, onChange = { seen.add(it) }, timeoutMs = 5000)
    try {
      journal.setWritable(false)
      val readOnly = try { File(journal, "probe").createNewFile(); false } catch (_: java.io.IOException) { true }
      org.junit.Assume.assumeTrue("needs a non-root user", readOnly)
      server.enqueue(MockResponse().setResponseCode(403).setBody("{\"code\":\"CAPABILITY_DENIED\"}"))
      e.enqueue("t-ro", "h", address, "/a.bin", dest("ro.bin"))
      val end = System.currentTimeMillis() + 10_000
      while (System.currentTimeMillis() < end && seen.none { it.state == TransferState.FAILED }) Thread.sleep(20)
      assertEquals("CAPABILITY_DENIED", seen.last { it.state == TransferState.FAILED }.error)
    } finally {
      journal.setWritable(true)
      e.shutdown()
    }
  }
}
