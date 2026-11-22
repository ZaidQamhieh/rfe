package expo.modules.rfetransport

import okhttp3.mockwebserver.Dispatcher
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import okhttp3.mockwebserver.RecordedRequest
import okhttp3.mockwebserver.SocketPolicy
import okhttp3.tls.HandshakeCertificates
import okhttp3.tls.HeldCertificate
import org.junit.After
import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import java.io.ByteArrayOutputStream
import java.io.File
import java.nio.file.Files
import java.security.MessageDigest
import java.util.concurrent.ConcurrentHashMap

/** The upload engine against a fake agent that speaks the `/v1/transfers` session protocol and checks every hash. */
class UploadTest {
  private lateinit var server: MockWebServer
  private lateinit var dir: File
  private lateinit var engine: TransferEngine
  private lateinit var agent: FakeAgent
  private var pin: String? = null
  private val chunk = 100_000
  private val payload = ByteArray(250_000) { (it * 7 % 253).toByte() }

  private val creds = object : Credentials {
    override fun pin(hostId: String) = pin
    override fun token(hostId: String) = "tok"
  }

  private fun hex(b: ByteArray) = MessageDigest.getInstance("SHA-256").digest(b).joinToString("") { "%02x".format(it) }

  /** Minimal agent: one session, chunks stored by index, hashes verified like the real one. */
  private inner class FakeAgent : Dispatcher() {
    var size = 0L
    var wholeSha = ""
    var chunkSize = 0
    var sessionOpen = false
    val received = ConcurrentHashMap<Int, ByteArray>()
    val putCount = ConcurrentHashMap<Int, Int>()
    var openStatus = 201
    var openCode = ""
    var failChunk = -1
    var corruptOnce = -1
    var completed = false
    var lastOverwrite = false
    /** POST /transfers calls seen: a finished upload must never open a second session. */
    val openCount = java.util.concurrent.atomic.AtomicInteger(0)
    /** The Idempotency-Key header of every POST /transfers, null where the client sent none. */
    val openKeys = java.util.concurrent.CopyOnWriteArrayList<String?>()
    /** How many open answers to lose: the session is created, then the connection drops before the 201. */
    val dropOpenAcks = java.util.concurrent.atomic.AtomicInteger(0)
    /** Refuse the next keyed open as the agent does for a key whose session is finished or describes other bytes. */
    @Volatile var refuseKeyOnce = false
    private var sessionKey: String? = null
    /** How many complete answers to lose: the complete is applied, then the connection drops before the 200. */
    val dropCompleteAcks = java.util.concurrent.atomic.AtomicInteger(0)
    /** Report a completed session as still open on the next GET, as a read that raced the complete would. */
    @Volatile var staleOpenOnce = false
    @Volatile var putDelayMs = 0L
    /** Chunk requests still to be refused as if an earlier, dead connection held the session. */
    val busyPuts = java.util.concurrent.atomic.AtomicInteger(0)
    /** Chunk requests still to be refused with 507 as a full disk would; [fullRetryAfter] is the Retry-After sent (null: none). */
    val fullPuts = java.util.concurrent.atomic.AtomicInteger(0)
    @Volatile var fullRetryAfter: String? = "0"
    val inFlight = java.util.concurrent.atomic.AtomicInteger(0)
    val maxInFlight = java.util.concurrent.atomic.AtomicInteger(0)

    private fun json(status: Int, body: String) = MockResponse().setResponseCode(status).setBody(body)
    private fun err(status: Int, code: String) = json(status, """{"code":"$code","message":"x"}""")

    override fun dispatch(request: RecordedRequest): MockResponse {
      assertEquals("Bearer tok", request.getHeader("Authorization"))
      val path = request.path!!
      return when {
        request.method == "POST" && path == "/v1/transfers" -> {
          openCount.incrementAndGet()
          val key = request.getHeader("Idempotency-Key")
          openKeys.add(key)
          if (openStatus != 201) return err(openStatus, openCode)
          val j = org.json.JSONObject(request.body.readUtf8())
          if (refuseKeyOnce && key != null) { refuseKeyOnce = false; return err(409, "IDEMPOTENCY_KEY_USED") }
          // Like the real agent: the same key for a session that is still open is that session, not a new one.
          if (key != null && key == sessionKey && sessionOpen) return json(200, session())
          // Like the real agent: a destination the finished upload already published is a conflict without overwrite.
          if (completed && !j.getBoolean("overwrite")) return err(409, "CONFLICT")
          size = j.getLong("size"); wholeSha = j.getString("sha256"); chunkSize = j.getInt("chunkSize"); lastOverwrite = j.getBoolean("overwrite")
          sessionOpen = true
          sessionKey = key
          if (dropOpenAcks.getAndUpdate { maxOf(it - 1, 0) } > 0) return MockResponse().setSocketPolicy(SocketPolicy.DISCONNECT_AFTER_REQUEST)
          json(201, session())
        }
        request.method == "GET" && path == "/v1/transfers/s1" -> if (!sessionOpen) err(404, "NOT_FOUND") else json(200, session())
        request.method == "PUT" && path.startsWith("/v1/transfers/s1/chunks/") -> {
          if (busyPuts.getAndUpdate { maxOf(it - 1, 0) } > 0) return err(409, "TRANSFER_ACTIVE")
          if (fullPuts.getAndUpdate { maxOf(it - 1, 0) } > 0) return err(507, "INSUFFICIENT_STORAGE").also { r -> fullRetryAfter?.let { r.setHeader("Retry-After", it) } }
          val n = path.substringAfterLast('/').toInt()
          putCount.merge(n, 1, Int::plus)
          val now = inFlight.incrementAndGet()
          maxInFlight.accumulateAndGet(now, ::maxOf)
          try { if (putDelayMs > 0) Thread.sleep(putDelayMs) } finally { inFlight.decrementAndGet() }
          if (n == failChunk) {
            // Refuse only once the sibling chunks (sent in parallel) have landed, so the test does not race the client
            // cancelling them mid-flight: a cancelled chunk the agent already stored may legitimately be sent again.
            val others = (size + chunkSize - 1) / chunkSize - 1
            val deadline = System.currentTimeMillis() + 15_000
            while (received.size < others && System.currentTimeMillis() < deadline) Thread.sleep(5)
            return err(403, "FORBIDDEN")
          }
          val body = request.body.readByteArray()
          val claimed = request.getHeader("X-Chunk-Sha256")
          val data = if (n == corruptOnce && putCount[n] == 1) body.copyOf().also { if (it.isNotEmpty()) it[0] = (it[0] + 1).toByte() } else body
          if (hex(data) != claimed) return err(409, "CHUNK_HASH_MISMATCH")
          received[n] = data
          MockResponse().setResponseCode(204)
        }
        request.method == "POST" && path == "/v1/transfers/s1/complete" -> {
          val all = ByteArrayOutputStream()
          received.toSortedMap().values.forEach { all.write(it) }
          if (hex(all.toByteArray()) != wholeSha) return err(422, "HASH_MISMATCH")
          // An agent that predates the idempotent complete: a repeat is refused once the session is completed.
          if (completed) return err(409, "TRANSFER_NOT_OPEN")
          completed = true
          if (dropCompleteAcks.getAndUpdate { maxOf(it - 1, 0) } > 0) return MockResponse().setSocketPolicy(SocketPolicy.DISCONNECT_AFTER_REQUEST)
          json(200, """{"path":"/x","sha256":"$wholeSha","verified":true}""")
        }
        else -> MockResponse().setResponseCode(500)
      }
    }

    fun assembled(): ByteArray = ByteArrayOutputStream().also { o -> received.toSortedMap().values.forEach { o.write(it) } }.toByteArray()

    private fun session(): String {
      val total = if (size == 0L) 1 else ((size + chunkSize - 1) / chunkSize).toInt()
      val status = if (completed && !staleOpenOnce) "completed" else "open"
      staleOpenOnce = false
      return """{"id":"s1","path":"/x","size":$size,"chunkSize":$chunkSize,"totalChunks":$total,"receivedChunks":[${received.keys.sorted().joinToString(",")}],"status":"$status"}"""
    }
  }

  @Before fun setUp() {
    val leaf = HeldCertificate.Builder().commonName("rfe-host").addSubjectAlternativeName("localhost").build()
    server = MockWebServer()
    server.useHttps(HandshakeCertificates.Builder().heldCertificate(leaf).build().sslSocketFactory(), false)
    agent = FakeAgent()
    server.dispatcher = agent
    server.start()
    pin = PinnedHttp.sha256Hex(leaf.certificate.encoded)
    dir = Files.createTempDirectory("rfe-upload").toFile()
    engine = TransferEngine(File(dir, "journal"), creds, timeoutMs = 5000, uploadChunkSize = chunk, busyDelayMs = 20)
  }

  @After fun tearDown() {
    engine.shutdown()
    server.shutdown()
    dir.deleteRecursively()
  }

  private val address get() = "${server.hostName}:${server.port}"

  private fun source(bytes: ByteArray = payload) = File(dir, "src.bin").also { it.writeBytes(bytes) }

  private fun await(id: String, vararg states: TransferState): TransferRecord {
    val end = System.currentTimeMillis() + 10_000
    while (System.currentTimeMillis() < end) {
      engine.list().firstOrNull { it.id == id && it.state in states }?.let { return it }
      Thread.sleep(20)
    }
    throw AssertionError("timeout waiting for $states, have ${engine.list()}")
  }

  /** DONE is published before the finished upload's source is removed (the safe order), so wait for the removal. */
  private fun awaitGone(f: File) {
    val deadline = System.currentTimeMillis() + 10_000
    while (f.exists() && System.currentTimeMillis() < deadline) Thread.sleep(20)
    assertFalse("${f.path} should have been removed", f.exists())
  }

  @Test fun uploadsInVerifiedChunksAndCompletes() {
    engine.enqueueUpload("u1", "h", address, source().path, "/dest/a.bin", overwrite = false, deleteSource = false)
    val r = await("u1", TransferState.DONE)
    assertArrayEquals(payload, agent.assembled())
    assertEquals(3, agent.received.size)
    assertTrue(agent.completed)
    assertEquals(payload.size.toLong(), r.received)
    assertEquals(payload.size.toLong(), r.total)
    assertEquals(hex(payload), r.sha256)
    assertEquals("UPLOAD", r.direction)
  }

  @Test fun waitsOutASessionThatStillHoldsAnEarlierConnection() {
    agent.busyPuts.set(3)
    engine.enqueueUpload("u1", "h", address, source().path, "/dest/a.bin", overwrite = false, deleteSource = false)
    await("u1", TransferState.DONE)
    assertArrayEquals(payload, agent.assembled())
  }

  @Test fun failsWhenTheSessionStaysBusy() {
    agent.busyPuts.set(1000)
    engine.enqueueUpload("u1", "h", address, source().path, "/dest/a.bin", overwrite = false, deleteSource = false)
    assertNotNull(await("u1", TransferState.FAILED).error)
  }

  @Test fun waitsOutAFullDiskAnswerWithRetryAfterAndFinishes() {
    agent.fullPuts.set(2)
    engine.enqueueUpload("u1", "h", address, source().path, "/dest/a.bin", overwrite = false, deleteSource = false)
    await("u1", TransferState.DONE)
    assertArrayEquals(payload, agent.assembled())
    assertTrue(agent.completed)
  }

  @Test fun aFullDiskAnswerWithoutRetryAfterIsFinal() {
    agent.fullPuts.set(1000)
    agent.fullRetryAfter = null
    engine.enqueueUpload("u1", "h", address, source().path, "/dest/a.bin", overwrite = false, deleteSource = false)
    assertEquals("INSUFFICIENT_STORAGE", await("u1", TransferState.FAILED).error)
  }

  @Test fun givesUpWhenTheDiskStaysFull() {
    agent.fullPuts.set(1000)
    engine.enqueueUpload("u1", "h", address, source().path, "/dest/a.bin", overwrite = false, deleteSource = false)
    assertEquals("INSUFFICIENT_STORAGE", await("u1", TransferState.FAILED).error)
  }

  @Test fun sendsSeveralChunksAtOnce() {
    agent.putDelayMs = 150
    engine.enqueueUpload("u9", "h", address, source().path, "/dest/a.bin", overwrite = false, deleteSource = false)
    await("u9", TransferState.DONE)
    assertArrayEquals(payload, agent.assembled())
    assertTrue("expected concurrent chunk PUTs, saw ${agent.maxInFlight.get()}", agent.maxInFlight.get() >= 2)
  }

  @Test fun resumesFromTheAgentsBitmapWithoutResendingChunks() {
    agent.failChunk = 1
    engine.enqueueUpload("u2", "h", address, source().path, "/dest/a.bin", overwrite = false, deleteSource = false)
    val failed = await("u2", TransferState.FAILED)
    assertEquals("FORBIDDEN", failed.error)
    assertEquals("s1", failed.sessionId)
    // Chunks go out a few at a time, so the last one may already have landed when chunk 1 is refused.
    assertTrue(failed.received == chunk.toLong() || failed.received == (payload.size - 2 * chunk + chunk).toLong())
    agent.failChunk = -1
    engine.resume("u2")
    await("u2", TransferState.DONE)
    assertArrayEquals(payload, agent.assembled())
    assertEquals("chunk 0 must not be sent twice", 1, agent.putCount[0])
    assertEquals(1, agent.putCount[2])
  }

  @Test fun startsANewSessionWhenTheAgentForgotTheOldOne() {
    agent.failChunk = 1
    engine.enqueueUpload("u3", "h", address, source().path, "/dest/a.bin", overwrite = false, deleteSource = false)
    await("u3", TransferState.FAILED)
    agent.sessionOpen = false
    agent.received.clear()
    agent.failChunk = -1
    engine.resume("u3")
    await("u3", TransferState.DONE)
    assertArrayEquals(payload, agent.assembled())
  }

  /** The complete reached the agent but its 200 was lost; the retry must find the session completed and be done. */
  private fun lostCompleteAckIsDone(overwrite: Boolean) {
    agent.dropCompleteAcks.set(1)
    engine.enqueueUpload("u1", "h", address, source().path, "/dest/a.bin", overwrite = overwrite, deleteSource = false)
    val first = await("u1", TransferState.DONE, TransferState.FAILED)
    if (first.state == TransferState.FAILED) {
      engine.resume("u1")
      await("u1", TransferState.DONE)
    }
    assertTrue(agent.completed)
    assertEquals("a published upload must not open a second session", 1, agent.openCount.get())
    assertArrayEquals(payload, agent.assembled())
    assertEquals("no chunk may be sent again", true, agent.putCount.values.all { it == 1 })
  }

  @Test fun aLostCompleteAckIsReportedDoneWithoutOpeningASecondSession() = lostCompleteAckIsDone(overwrite = false)

  @Test fun aLostCompleteAckIsNotReUploadedWhenOverwriting() = lostCompleteAckIsDone(overwrite = true)

  @Test fun aCompleteRefusedAsNotOpenOnACompletedSessionIsDone() {
    agent.dropCompleteAcks.set(1)
    engine.enqueueUpload("u1", "h", address, source().path, "/dest/a.bin", overwrite = false, deleteSource = false)
    val first = await("u1", TransferState.DONE, TransferState.FAILED)
    if (first.state == TransferState.FAILED) {
      agent.staleOpenOnce = true
      engine.resume("u1")
      await("u1", TransferState.DONE)
    }
    assertEquals(1, agent.openCount.get())
  }

  /** The agent made the session but its 201 was lost: the retry must present the same key and so get the same session. */
  @Test fun aLostOpenAckIsRetriedWithTheSameKeyAndFindsTheSameSession() {
    agent.dropOpenAcks.set(1)
    engine.enqueueUpload("u1", "h", address, source().path, "/dest/a.bin", overwrite = false, deleteSource = false)
    val first = await("u1", TransferState.DONE, TransferState.FAILED)
    if (first.state == TransferState.FAILED) {
      assertNotNull("the key must be journaled before the POST", first.idempotencyKey)
      engine.resume("u1")
      await("u1", TransferState.DONE)
    }
    val keys = agent.openKeys.toList()
    assertEquals("the lost open and its retry", 2, keys.size)
    assertTrue("a 128-bit hex key, not '${keys[0]}'", Regex("[0-9a-f]{32}").matches(keys[0] ?: ""))
    assertEquals("a retry must reuse the key", keys[0], keys[1])
    assertEquals(keys[0], engine.list().first { it.id == "u1" }.idempotencyKey)
    assertArrayEquals(payload, agent.assembled())
  }

  @Test fun eachUploadGetsItsOwnKey() {
    engine.enqueueUpload("u1", "h", address, source().path, "/dest/a.bin", overwrite = true, deleteSource = false)
    await("u1", TransferState.DONE)
    engine.enqueueUpload("u2", "h", address, source().path, "/dest/b.bin", overwrite = true, deleteSource = false)
    await("u2", TransferState.DONE)
    val keys = agent.openKeys.toList()
    assertEquals(2, keys.size)
    assertTrue(keys.all { it != null })
    assertTrue("two uploads shared a key", keys[0] != keys[1])
  }

  /** The agent says the key is spent (its session finished or failed): this is a new attempt and takes a new key. */
  @Test fun aKeyTheAgentRefusesAsUsedIsReplacedAndTheOpenRetried() {
    agent.refuseKeyOnce = true
    engine.enqueueUpload("u1", "h", address, source().path, "/dest/a.bin", overwrite = false, deleteSource = false)
    await("u1", TransferState.DONE)
    val keys = agent.openKeys.toList()
    assertEquals(2, keys.size)
    assertTrue("the retry must use a fresh key", keys[0] != keys[1])
    assertArrayEquals(payload, agent.assembled())
  }

  @Test fun resendsAChunkThatArrivedCorrupted() {
    agent.corruptOnce = 1
    engine.enqueueUpload("u4", "h", address, source().path, "/dest/a.bin", overwrite = false, deleteSource = false)
    await("u4", TransferState.DONE)
    assertEquals(2, agent.putCount[1])
    assertArrayEquals(payload, agent.assembled())
  }

  @Test fun reportsTheAgentsRefusalAndKeepsTheSource() {
    agent.openStatus = 409
    agent.openCode = "CONFLICT"
    val src = source()
    engine.enqueueUpload("u5", "h", address, src.path, "/dest/a.bin", overwrite = false, deleteSource = true)
    val r = await("u5", TransferState.FAILED)
    assertEquals("CONFLICT", r.error)
    assertTrue("a failed upload stays retryable", src.exists())
  }

  @Test fun sendsAnEmptyFileAsOneEmptyChunk() {
    engine.enqueueUpload("u6", "h", address, source(ByteArray(0)).path, "/dest/e", overwrite = true, deleteSource = false)
    await("u6", TransferState.DONE)
    assertEquals(1, agent.received.size)
    assertEquals(0, agent.assembled().size)
    assertTrue(agent.lastOverwrite)
  }

  @Test fun dropsTheStagingFolderWithTheSource() {
    val staged = File(dir, "uploads/u11").also { it.mkdirs() }
    val src = File(staged, "a.bin").also { it.writeBytes(payload) }
    engine.enqueueUpload("u11", "h", address, src.path, "/dest/a.bin", overwrite = false, deleteSource = true)
    await("u11", TransferState.DONE)
    awaitGone(staged)
  }

  @Test fun deletesAnAppPrivateSourceOnceDoneAndOnCancel() {
    val src = source()
    engine.enqueueUpload("u7", "h", address, src.path, "/dest/a.bin", overwrite = false, deleteSource = true)
    await("u7", TransferState.DONE)
    awaitGone(src)

    val second = File(dir, "second.bin").also { it.writeBytes(payload) }
    agent.received.clear()
    agent.failChunk = 0
    engine.enqueueUpload("u8", "h", address, second.path, "/dest/b.bin", overwrite = false, deleteSource = true)
    await("u8", TransferState.FAILED)
    engine.cancel("u8")
    assertFalse(second.exists())
    assertNotNull(engine.list().first { it.id == "u8" })
  }

  @Test fun aChangedSourceIsHashedAgainAndOpensAFreshSession() {
    val src = source()
    agent.failChunk = 1
    engine.enqueueUpload("u9", "h", address, src.path, "/dest/a.bin", overwrite = false, deleteSource = false)
    await("u9", TransferState.FAILED)
    val edited = ByteArray(payload.size + 10) { (it % 199).toByte() }
    src.writeBytes(edited)
    agent.received.clear()
    agent.failChunk = -1
    engine.resume("u9")
    val r = await("u9", TransferState.DONE)
    assertArrayEquals(edited, agent.assembled())
    assertEquals(hex(edited), r.sha256)
  }

  @Test fun aSameSizeEditWithANewModifiedTimeIsHashedAgain() {
    val src = source()
    val stamp = src.lastModified()
    agent.failChunk = 1
    engine.enqueueUpload("u9b", "h", address, src.path, "/dest/a.bin", overwrite = false, deleteSource = false)
    await("u9b", TransferState.FAILED)
    val edited = ByteArray(payload.size) { (it % 197).toByte() }
    src.writeBytes(edited)
    src.setLastModified(stamp + 60_000)
    agent.received.clear()
    agent.failChunk = -1
    engine.resume("u9b")
    val r = await("u9b", TransferState.DONE)
    assertArrayEquals(edited, agent.assembled())
    assertEquals(hex(edited), r.sha256)
  }

  @Test fun aHashMismatchIsRecoveredByHashingAgainOnRetry() {
    val src = source()
    val stamp = src.lastModified()
    agent.failChunk = 1
    engine.enqueueUpload("u9c", "h", address, src.path, "/dest/a.bin", overwrite = false, deleteSource = false)
    await("u9c", TransferState.FAILED)
    // Edited in place with the same size and the same modified time: only the agent's check can notice.
    val edited = ByteArray(payload.size) { (it % 197).toByte() }
    src.writeBytes(edited)
    src.setLastModified(stamp)
    agent.failChunk = -1
    engine.resume("u9c")
    val failed = await("u9c", TransferState.FAILED)
    assertEquals("HASH_MISMATCH", failed.error)
    assertEquals(null, failed.sha256)
    agent.received.clear()
    engine.resume("u9c")
    val r = await("u9c", TransferState.DONE)
    assertArrayEquals(edited, agent.assembled())
    assertEquals(hex(edited), r.sha256)
  }

  @Test fun removeForgetsFinishedTransfersOnly() {
    engine.enqueueUpload("u10", "h", address, source().path, "/dest/a.bin", overwrite = false, deleteSource = false)
    await("u10", TransferState.DONE)
    engine.remove("u10")
    assertTrue(engine.list().none { it.id == "u10" })
    assertFalse(File(dir, "journal/u10.json").exists())
  }
}
