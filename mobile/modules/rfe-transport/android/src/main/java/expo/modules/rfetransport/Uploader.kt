package expo.modules.rfetransport

import okhttp3.Call
import okhttp3.MediaType.Companion.toMediaTypeOrNull
import okhttp3.RequestBody.Companion.toRequestBody
import org.json.JSONArray
import org.json.JSONObject
import java.io.File
import java.io.IOException
import java.io.RandomAccessFile
import java.security.MessageDigest
import java.util.concurrent.ConcurrentHashMap
import java.util.concurrent.Executors
import java.util.concurrent.atomic.AtomicInteger
import java.util.concurrent.atomic.AtomicReference

/** The agent refused an upload or download with a definite answer; [message] is its API error code (CONFLICT, HASH_MISMATCH...). */
class UploadRejected(val status: Int, code: String) : IOException(code.ifEmpty { "HTTP $status" })

/**
 * One resumable chunked upload against the agent's `/v1/transfers` session API.
 *
 * The whole file is hashed once (and the digest kept in the journal); each chunk carries its own SHA-256 and the
 * agent verifies both, so a corrupt byte anywhere fails loudly instead of landing on the host. Resume asks the agent
 * which chunks it already holds (its bitmap) and sends only the rest. Chunk sends retry on transport errors and on a
 * chunk hash mismatch; any other agent answer is final.
 */
class Uploader(
  private val creds: Credentials,
  private val timeoutMs: Long,
  private val stopped: () -> Boolean,
  private val track: (Call) -> Unit,
  private val save: (TransferRecord) -> Unit,
  private val chunkSize: Int = 4 * 1024 * 1024,
  private val chunkRetries: Int = 3,
  private val parallel: Int = 3,
  /** How often, and how long apart, a request is repeated while the agent still holds the session for an earlier, dead connection. */
  private val busyRetries: Int = 8,
  private val busyDelayMs: Long = 5_000,
) {
  private val live = ConcurrentHashMap.newKeySet<Call>()

  private class Session(val id: String, val chunkSize: Int, val totalChunks: Int, val received: Set<Int>, val open: Boolean)

  fun run(start: TransferRecord): TransferRecord {
    var r = start
    val src = File(r.destPath)
    if (!src.isFile) throw IOException("source file is missing")
    val size = src.length()
    val modified = src.lastModified()
    if (r.sha256 == null || r.total != size || r.sourceModified != modified) {
      // First run, or the source changed (size or modified time) since it was hashed: any old session describes different bytes.
      r = r.copy(sha256 = sha256Of(src), total = size, sourceModified = modified, sessionId = null)
      save(r)
    }
    var session: Session? = r.sessionId?.let { existing(r, it) }
    if (session == null) {
      session = open(r, size)
      r = r.copy(sessionId = session.id)
      save(r)
    }
    val open = session
    val base = r
    var sent = open.received.sumOf { chunkLen(it, size, open.chunkSize, open.totalChunks) }
    r = r.copy(received = sent)
    save(r)
    val pending = (0 until open.totalChunks).filter { it !in open.received }
    val next = AtomicInteger(0)
    val failure = AtomicReference<Throwable?>(null)
    val workers = minOf(parallel, pending.size)
    val pool = Executors.newFixedThreadPool(maxOf(workers, 1))
    try {
      // Chunks are independent on the agent (each holds an activity lease), so a few in flight hide round-trip latency.
      val jobs = (0 until workers).map {
        pool.submit {
          RandomAccessFile(src, "r").use { f ->
            try {
              while (failure.get() == null) {
                if (stopped()) throw InterruptedException()
                val i = next.getAndIncrement()
                if (i >= pending.size) break
                val n = pending[i]
                val len = chunkLen(n, size, open.chunkSize, open.totalChunks).toInt()
                val buf = ByteArray(len)
                f.seek(n.toLong() * open.chunkSize)
                f.readFully(buf)
                putChunk(base, open.id, n, buf)
                synchronized(this) {
                  sent += len
                  r = r.copy(received = sent)
                  save(r)
                }
              }
            } catch (e: Throwable) {
              if (failure.compareAndSet(null, e)) live.forEach { it.cancel() }
            }
          }
        }
      }
      try {
        jobs.forEach { it.get() }
      } catch (e: InterruptedException) {
        failure.compareAndSet(null, e)
      }
      failure.get()?.let { throw it }
    } finally {
      live.forEach { it.cancel() }
      pool.shutdownNow()
    }
    if (stopped()) throw InterruptedException()
    try {
      complete(r, session.id)
    } catch (e: UploadRejected) {
      // The bytes the agent assembled differ from the saved hash (e.g. the file was edited in place): hash again on retry.
      if (e.message == "HASH_MISMATCH") save(r.copy(sha256 = null, sessionId = null))
      throw e
    }
    return r.copy(received = size)
  }

  private fun chunkLen(n: Int, size: Long, chunk: Int, total: Int): Long =
    if (size == 0L) 0 else if (n == total - 1) size - n.toLong() * chunk else chunk.toLong()

  private fun sha256Of(f: File): String {
    val md = MessageDigest.getInstance("SHA-256")
    f.inputStream().use { input ->
      val buf = ByteArray(64 * 1024)
      while (true) {
        if (stopped()) throw InterruptedException()
        val n = input.read(buf)
        if (n < 0) break
        md.update(buf, 0, n)
      }
    }
    return md.digest().joinToString("") { "%02x".format(it) }
  }

  private class Reply(val status: Int, val body: String) {
    fun json(): JSONObject = JSONObject(body)
    fun code(): String = runCatching { JSONObject(body).optString("code") }.getOrDefault("")
  }

  /**
   * A dropped connection can leave the agent still waiting on the old request, and it answers 409 TRANSFER_ACTIVE to
   * anything else on that session until the old one times out. That passes on its own, so wait and ask again.
   */
  private fun send(r: TransferRecord, method: String, path: String, json: JSONObject? = null, raw: ByteArray? = null, extra: Map<String, String> = emptyMap()): Reply {
    var busy = 0
    while (true) {
      val reply = sendOnce(r, method, path, json, raw, extra)
      if (reply.status != 409 || reply.code() != "TRANSFER_ACTIVE" || busy >= busyRetries) return reply
      busy++
      var waited = 0L
      while (waited < busyDelayMs) {
        if (stopped()) throw InterruptedException()
        Thread.sleep(minOf(200L, busyDelayMs - waited))
        waited += 200L
      }
    }
  }

  private fun sendOnce(r: TransferRecord, method: String, path: String, json: JSONObject?, raw: ByteArray?, extra: Map<String, String>): Reply {
    val headers = mutableMapOf("X-RFE-Client-Version" to "rn")
    creds.token(r.hostId)?.let { headers["Authorization"] = "Bearer $it" }
    headers.putAll(extra)
    val body = when {
      json != null -> json.toString().toRequestBody("application/json".toMediaTypeOrNull())
      raw != null -> raw.toRequestBody("application/octet-stream".toMediaTypeOrNull())
      else -> null
    }
    val call = PinnedHttp.newCall(method, "https://${r.address}/v1$path", headers, body, creds.pin(r.hostId), timeoutMs)
    track(call)
    live.add(call)
    try {
      call.execute().use { resp -> return Reply(resp.code, resp.body?.string() ?: "") }
    } finally {
      live.remove(call)
    }
  }

  private fun rejected(reply: Reply): Nothing = throw UploadRejected(reply.status, reply.code())

  private fun parse(reply: Reply): Session {
    val j = reply.json()
    val got = j.optJSONArray("receivedChunks") ?: JSONArray()
    return Session(j.getString("id"), j.getInt("chunkSize"), j.getInt("totalChunks"), (0 until got.length()).map { got.getInt(it) }.toSet(), j.optString("status", "open") == "open")
  }

  /** The saved session if the agent still holds it and it is open; null means open a new one. */
  private fun existing(r: TransferRecord, id: String): Session? {
    val reply = send(r, "GET", "/transfers/$id")
    if (reply.status == 404) return null
    if (reply.status != 200) rejected(reply)
    return parse(reply).takeIf { it.open }
  }

  private fun open(r: TransferRecord, size: Long): Session {
    val reply = send(r, "POST", "/transfers", json = JSONObject()
      .put("path", r.remotePath).put("size", size).put("sha256", r.sha256).put("chunkSize", chunkSize).put("overwrite", r.overwrite))
    if (reply.status != 201) rejected(reply)
    return parse(reply)
  }

  private fun putChunk(r: TransferRecord, id: String, n: Int, data: ByteArray) {
    val sum = MessageDigest.getInstance("SHA-256").digest(data).joinToString("") { "%02x".format(it) }
    var attempt = 0
    while (true) {
      if (stopped()) throw InterruptedException()
      try {
        val reply = send(r, "PUT", "/transfers/$id/chunks/$n", raw = data, extra = mapOf("X-Chunk-Sha256" to sum))
        if (reply.status == 204) return
        // A corrupted or torn chunk is worth sending again; every other answer is the agent's decision.
        if (reply.code() != "CHUNK_HASH_MISMATCH" || attempt >= chunkRetries) rejected(reply)
      } catch (e: UploadRejected) {
        throw e
      } catch (e: IOException) {
        if (stopped() || e is javax.net.ssl.SSLException || attempt >= chunkRetries) throw e
      }
      attempt++
      Thread.sleep(200L * attempt)
    }
  }

  private fun complete(r: TransferRecord, id: String) {
    val reply = send(r, "POST", "/transfers/$id/complete")
    if (reply.status != 200) rejected(reply)
  }
}
