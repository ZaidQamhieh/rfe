package expo.modules.rfetransport

import org.json.JSONObject
import java.io.File
import java.io.FileOutputStream
import java.io.IOException
import java.util.concurrent.ConcurrentHashMap
import java.util.concurrent.Executors
import java.util.concurrent.Future

enum class TransferState { QUEUED, RUNNING, PAUSED, DONE, FAILED, CANCELLED }

data class TransferRecord(
  val id: String,
  val hostId: String,
  val address: String,
  val remotePath: String,
  val destPath: String,
  val state: TransferState,
  val received: Long,
  val total: Long,
  val error: String?,
  /** DOWNLOAD (default, also for journals written before uploads existed) or UPLOAD. */
  val direction: String = "DOWNLOAD",
  /** Upload only: the agent's session id, kept so a restart resumes the same session. */
  val sessionId: String? = null,
  /** Upload only: whole-file SHA-256 of the source at [total] bytes. */
  val sha256: String? = null,
  val overwrite: Boolean = false,
  /** Upload only: delete the local source once the upload is done or cancelled (app-private copies). */
  val deleteSource: Boolean = false,
  /** Downloads only: content URI of the copy published to the shared Downloads folder, when there is one. */
  val publicUri: String? = null,
  /** Epoch millis of the last journal write; for a finished transfer, when it finished. 0 in journals written before this field. */
  val updatedAt: Long = 0,
  /** Downloads only: the file's ETag (or Last-Modified) from the response that started the `.part`, sent as If-Range on resume. */
  val validator: String? = null,
  /** Upload only: the source's modified time when [sha256] was computed; a different time means the hash is stale. */
  val sourceModified: Long = 0,
) {
  val isUpload get() = direction == "UPLOAD"

  fun toJson(): JSONObject = JSONObject()
    .put("id", id).put("hostId", hostId).put("address", address).put("remotePath", remotePath)
    .put("destPath", destPath).put("state", state.name).put("received", received).put("total", total)
    .put("error", error ?: JSONObject.NULL).put("direction", direction)
    .put("sessionId", sessionId ?: JSONObject.NULL).put("sha256", sha256 ?: JSONObject.NULL)
    .put("overwrite", overwrite).put("deleteSource", deleteSource)
    .put("publicUri", publicUri ?: JSONObject.NULL).put("updatedAt", updatedAt)
    .put("validator", validator ?: JSONObject.NULL).put("sourceModified", sourceModified)

  companion object {
    fun fromJson(j: JSONObject) = TransferRecord(
      j.getString("id"), j.getString("hostId"), j.getString("address"), j.getString("remotePath"),
      j.getString("destPath"), TransferState.valueOf(j.getString("state")), j.getLong("received"),
      j.getLong("total"), if (j.isNull("error")) null else j.getString("error"),
      j.optString("direction", "DOWNLOAD"),
      if (j.isNull("sessionId")) null else j.optString("sessionId"),
      if (j.isNull("sha256")) null else j.optString("sha256"),
      j.optBoolean("overwrite", false), j.optBoolean("deleteSource", false),
      if (j.isNull("publicUri")) null else j.optString("publicUri"),
      j.optLong("updatedAt", 0),
      if (j.isNull("validator")) null else j.optString("validator"),
      j.optLong("sourceModified", 0),
    )
  }
}

/** Supplies the secure-store pin and token; secrets are never persisted in the journal. */
interface Credentials {
  fun pin(hostId: String): String?
  fun token(hostId: String): String?
}

/**
 * Durable, resumable download engine. Independent of the JS runtime and of any
 * Android UI so a foreground service can keep it alive when the app is
 * backgrounded or killed. State lives in a per-transfer JSON journal; the bytes
 * live in `<dest>.part` and are the source of truth for resume offsets.
 *
 * - Resume: Range from the `.part` length; a 200 to a ranged request restarts
 *   from zero (server ignored Range), never appends full content to a partial.
 * - Integrity: when the server states a length, the final size must match
 *   before the atomic rename; a mismatch fails the transfer and keeps `.part`.
 * - Process death: transfers found RUNNING/QUEUED at startup become PAUSED.
 */
class TransferEngine(
  private val dir: File,
  private val creds: Credentials,
  private val maxConcurrent: Int = 2,
  private val onChange: (TransferRecord) -> Unit = {},
  private val timeoutMs: Long = 30000,
  private val uploadChunkSize: Int = 4 * 1024 * 1024,
  private val busyDelayMs: Long = 5_000,
  /** Copies a finished download somewhere shared and returns its URI; on success the private copy is dropped. */
  private val publisher: ((File) -> String?)? = null,
) {
  private val pool = Executors.newFixedThreadPool(maxConcurrent)
  private val records = ConcurrentHashMap<String, TransferRecord>()
  private val futures = ConcurrentHashMap<String, Future<*>>()
  private val calls = ConcurrentHashMap<String, okhttp3.Call>()
  private class Attempt { @Volatile var stop: TransferState? = null }
  private val attempts = ConcurrentHashMap<String, Attempt>()

  init {
    dir.mkdirs()
    dir.listFiles { f -> f.extension == "json" }?.forEach { f ->
      runCatching { TransferRecord.fromJson(JSONObject(f.readText())) }.getOrNull()?.let { r ->
        val fixed = if (r.state == TransferState.RUNNING || r.state == TransferState.QUEUED) r.copy(state = TransferState.PAUSED) else r
        records[fixed.id] = fixed
        if (fixed != r) persist(fixed)
      }
    }
  }

  fun list(): List<TransferRecord> = records.values.sortedBy { it.id }
  fun activeCount(): Int = records.values.count { it.state == TransferState.RUNNING || it.state == TransferState.QUEUED }

  fun enqueue(id: String, hostId: String, address: String, remotePath: String, destPath: String): TransferRecord {
    require(id.matches(Regex("[A-Za-z0-9_-]{1,64}"))) { "invalid transfer id" }
    requireIdFree(id)
    val r = TransferRecord(id, hostId, address, remotePath, destPath, TransferState.QUEUED, 0, -1, null)
    records[id] = r
    persist(r)
    submit(id)
    return r
  }

  /**
   * Queues a resumable chunked upload of [localPath] to [remotePath] on the host. For uploads [destPath] holds the
   * local source file and [remotePath] the target path on the host.
   */
  fun enqueueUpload(id: String, hostId: String, address: String, localPath: String, remotePath: String, overwrite: Boolean, deleteSource: Boolean): TransferRecord {
    require(id.matches(Regex("[A-Za-z0-9_-]{1,64}"))) { "invalid transfer id" }
    requireIdFree(id)
    val r = TransferRecord(id, hostId, address, remotePath, localPath, TransferState.QUEUED, 0, -1, null, "UPLOAD", null, null, overwrite, deleteSource)
    records[id] = r
    persist(r)
    submit(id)
    return r
  }

  /** A live transfer's id is not reused: a second one would overwrite its future and share its partial file (audit F-116). */
  private fun requireIdFree(id: String) {
    val cur = records[id] ?: return
    require(cur.state != TransferState.RUNNING && cur.state != TransferState.QUEUED) { "transfer id already in use" }
  }

  fun resume(id: String) {
    val r = records[id] ?: return
    if (r.state == TransferState.PAUSED || r.state == TransferState.FAILED) {
      update(r.copy(state = TransferState.QUEUED, error = null))
      submit(id)
    }
  }

  fun pause(id: String) = stop(id, TransferState.PAUSED)

  fun cancel(id: String) {
    stop(id, TransferState.CANCELLED)
    records[id]?.let { if (it.isUpload) discardSource(it) else dropPartial(it) }
  }

  /** Forgets a finished, failed or cancelled transfer (journal entry and any leftover partial); running ones are left alone. */
  fun remove(id: String) {
    val r = records[id] ?: return
    if (r.state == TransferState.RUNNING || r.state == TransferState.QUEUED) return
    records.remove(id)
    attempts.remove(id)
    futures.remove(id)
    File(dir, "$id.json").delete()
    if (r.isUpload) discardSource(r) else if (r.state != TransferState.DONE) dropPartial(r)
  }

  private fun dropPartial(r: TransferRecord) {
    File(r.destPath + ".part").delete()
    dropStagingDir(File(r.destPath), r.id)
  }

  /** Each download stages in its own folder named after the transfer, so two downloads of one file never share a `.part`. */
  private fun dropStagingDir(dest: File, id: String) {
    dest.parentFile?.takeIf { it.name == id }?.delete()
  }

  fun shutdown() {
    pool.shutdownNow()
  }

  private fun stop(id: String, reason: TransferState) {
    val r = records[id] ?: return
    if (r.state == TransferState.DONE) return
    attempts[id]?.stop = reason
    calls[id]?.cancel()
    futures[id]?.cancel(true)
    if (r.state != TransferState.RUNNING) update(r.copy(state = reason))
  }

  private fun submit(id: String) {
    val attempt = Attempt()
    attempts[id] = attempt
    futures[id] = pool.submit { run(id, attempt) }
  }

  private fun discardSource(r: TransferRecord) {
    if (!r.deleteSource) return
    val f = File(r.destPath)
    f.delete()
    // Each staged upload lives in its own folder named after the transfer; drop it with the file.
    f.parentFile?.takeIf { it.name == r.id }?.delete()
  }

  private fun runUpload(id: String, attempt: Attempt) {
    var r = records[id] ?: return
    if (attempt.stop != null) return
    r = update(r.copy(state = TransferState.RUNNING, error = null))
    try {
      val uploader = Uploader(
        creds, timeoutMs,
        stopped = { Thread.currentThread().isInterrupted || attempt.stop != null },
        track = { calls[id] = it },
        // A worker that was stopped and replaced (pause, then an immediate resume) must not write progress or
        // finish the transfer under the new attempt (audit F-126).
        save = { next -> if (attempts[id] === attempt) r = update(next) },
        chunkSize = uploadChunkSize,
        busyDelayMs = busyDelayMs,
      )
      r = uploader.run(r)
      if (attempts[id] !== attempt) return // replaced while finishing: the new attempt still needs the source
      update(r.copy(state = TransferState.DONE, received = r.total, error = null))
      // The upload is done; a failure to remove the staged source must not turn it back into FAILED (audit F-127).
      runCatching { discardSource(r) }
    } catch (e: Exception) {
      if (attempts[id] !== attempt) return
      val reason = attempt.stop ?: if (e is InterruptedException || Thread.currentThread().isInterrupted) TransferState.PAUSED else null
      val cur = records[id] ?: return
      if (reason != null) update(cur.copy(state = reason))
      else update(cur.copy(state = TransferState.FAILED, error = describe(e)))
    } finally {
      if (attempts[id] === attempt) calls.remove(id)
    }
  }

  private fun run(id: String, attempt: Attempt) {
    if (records[id]?.isUpload == true) return runUpload(id, attempt)
    var r = records[id] ?: return
    if (attempt.stop != null) return
    r = update(r.copy(state = TransferState.RUNNING, error = null))
    val dest = File(r.destPath)
    val part = File(dest.path + ".part")
    try {
      dest.parentFile?.mkdirs()
      val token = creds.token(r.hostId)
      val headers = mutableMapOf("X-RFE-Client-Version" to "rn")
      if (token != null) headers["Authorization"] = "Bearer $token"
      var offset = if (part.exists()) part.length() else 0L
      // A partial can only be continued if we know which version of the file it came from; otherwise start over.
      if (offset > 0 && r.validator == null) { part.delete(); offset = 0 }
      if (offset > 0) {
        headers["Range"] = "bytes=$offset-"
        headers["If-Range"] = r.validator!!
      }
      val url = "https://${r.address}/v1/content?path=" + java.net.URLEncoder.encode(r.remotePath, "UTF-8")
      val call = PinnedHttp.newGetCall(url, headers, creds.pin(r.hostId), timeoutMs)
      calls[id] = call
      call.execute().use { resp ->
        if (resp.code == 416 && offset > 0) { // stale/oversized partial: restart cleanly
          part.delete()
          throw RestartFromZero()
        }
        if (!resp.isSuccessful) {
          // A definite refusal (no permission, file gone) carries the agent's error code; anything else is a transient failure.
          val definite = resp.code in 400..499 && resp.code != 408 && resp.code != 429
          if (!definite) throw IOException("HTTP ${resp.code}")
          val code = runCatching { org.json.JSONObject(resp.body?.string() ?: "").optString("code") }.getOrDefault("")
          throw UploadRejected(resp.code, code)
        }
        val resumed = offset > 0 && resp.code == 206
        if (!resumed) {
          // 200: the file changed (If-Range failed) or the server ignored Range; the body is the whole current file.
          offset = 0
          r = update(r.copy(validator = resp.header("ETag") ?: resp.header("Last-Modified")))
        }
        val len = resp.body!!.contentLength()
        val total = if (len >= 0) offset + len else -1L
        var received = offset
        var lastEmit = 0L
        FileOutputStream(part, resumed).use { out ->
          val buf = ByteArray(64 * 1024)
          val src = resp.body!!.byteStream()
          while (true) {
            if (Thread.currentThread().isInterrupted || attempt.stop != null) throw InterruptedException()
            val n = src.read(buf)
            if (n < 0) break
            out.write(buf, 0, n)
            received += n
            val now = System.currentTimeMillis()
            if (now - lastEmit > 250) {
              lastEmit = now
              if (attempts[id] === attempt) r = update(r.copy(received = received, total = total))
            }
          }
          out.fd.sync()
        }
        if (total >= 0 && part.length() != total) throw IOException("size mismatch: got ${part.length()} expected $total")
        // A worker that was stopped (or replaced by a resume) in its last moments must not publish or finish
        // the transfer under the new attempt; the `.part` stays for the resume.
        if (attempt.stop != null || attempts[id] !== attempt) throw InterruptedException()
        if (dest.exists()) dest.delete()
        if (!part.renameTo(dest)) throw IOException("could not finalize download")
        val size = dest.length()
        // Publishing is best effort: if it fails the download simply stays in app storage.
        val published = publisher?.let { p -> runCatching { p(dest) }.getOrNull() }
        if (published != null) { dest.delete(); dropStagingDir(dest, id) }
        update(r.copy(state = TransferState.DONE, received = size, total = size, error = null, publicUri = published))
      }
    } catch (e: RestartFromZero) {
      if (attempts[id] === attempt) submit(id)
    } catch (e: Exception) {
      if (attempts[id] !== attempt) return // superseded by a newer attempt
      // An interrupt without an explicit stop is engine shutdown: keep the partial, resumable.
      val reason = attempt.stop ?: if (e is InterruptedException || Thread.currentThread().isInterrupted) TransferState.PAUSED else null
      val cur = records[id] ?: return
      if (reason != null) update(cur.copy(state = reason))
      else update(cur.copy(state = TransferState.FAILED, error = describe(e)))
    } finally {
      if (attempts[id] === attempt) calls.remove(id)
    }
  }

  private class RestartFromZero : RuntimeException()

  private fun describe(e: Exception): String = when (e) {
    is CertPinMismatch -> "ERR_CERT_PIN_MISMATCH"
    is PinPolicyViolation -> "ERR_PIN_POLICY"
    is javax.net.ssl.SSLException ->
      if (generateSequence<Throwable>(e) { it.cause }.any { it is CertPinMismatch }) "ERR_CERT_PIN_MISMATCH" else "ERR_TLS"
    is UploadRejected -> e.message ?: "ERR_UPLOAD"
    is IOException -> if (isNoSpace(e)) "ERR_STORAGE_FULL" else "ERR_CONNECTION: ${e.message}"
    else -> e.message ?: e.javaClass.simpleName
  }

  private fun isNoSpace(e: Throwable): Boolean = generateSequence(e) { it.cause }.any {
    val m = it.message ?: return@any false
    m.contains("ENOSPC") || m.contains("No space left", ignoreCase = true)
  }

  private fun update(next: TransferRecord): TransferRecord {
    val r = next.copy(updatedAt = System.currentTimeMillis())
    records[r.id] = r
    persist(r)
    onChange(r)
    return r
  }

  /** Best effort: a full disk fails the journal write as well, and that must not hide the transfer's own state from the UI. */
  private fun persist(r: TransferRecord) {
    try {
      val tmp = File(dir, "${r.id}.json.tmp")
      tmp.writeText(r.toJson().toString())
      tmp.renameTo(File(dir, "${r.id}.json"))
    } catch (_: IOException) {
    }
  }
}
