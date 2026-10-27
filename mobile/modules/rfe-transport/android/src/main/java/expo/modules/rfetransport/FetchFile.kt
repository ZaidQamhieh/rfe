package expo.modules.rfetransport

import java.io.File
import java.io.FileOutputStream
import java.util.concurrent.ConcurrentHashMap

data class FetchResult(val status: Int, val retryAfterSeconds: Int?, val bytes: Long)

/** The body grew past the caller's cap; the partial file is discarded (never trust a remote size). */
class FetchTooLarge(val maxBytes: Long) : java.io.IOException("response exceeds $maxBytes bytes")

/**
 * Cancellable pinned GET-to-file for small assets (thumbnails). Unlike [PinnedHttp.downloadToFile] a
 * non-2xx status is a normal result (404 = "no thumbnail", 429 = busy + Retry-After), and a running
 * fetch can be cancelled by id when no view needs it any more. The file is only published on 2xx; concurrent fetches of one
 * destination each write their own partial and the last complete one wins.
 */
object FetchFile {
  private val calls = ConcurrentHashMap<String, okhttp3.Call>()

  fun get(id: String, url: String, headers: Map<String, String>, pin: String?, dest: File, timeoutMs: Long = 20000, maxBytes: Long = 0): FetchResult {
    val call = PinnedHttp.newGetCall(url, headers, pin, timeoutMs)
    calls[id] = call
    // One partial file per fetch id, so two fetches of the same destination (viewer and neighbour preload) never interleave.
    val part = File(dest.path + "." + id.filter { it.isLetterOrDigit() } + ".part")
    try {
      call.execute().use { resp ->
        if (!resp.isSuccessful) return FetchResult(resp.code, resp.header("Retry-After")?.toIntOrNull(), 0)
        dest.parentFile?.mkdirs()
        var n = 0L
        FileOutputStream(part).use { out ->
          val src = resp.body!!.byteStream()
          val buf = ByteArray(32 * 1024)
          while (true) {
            val r = src.read(buf)
            if (r < 0) break
            n += r
            if (maxBytes > 0 && n > maxBytes) throw FetchTooLarge(maxBytes)
            out.write(buf, 0, r)
          }
        }
        if (!part.renameTo(dest)) throw java.io.IOException("could not publish file")
        return FetchResult(resp.code, null, n)
      }
    } catch (e: Exception) {
      part.delete()
      throw e
    } finally {
      // Only this fetch's own handle: a newer fetch with the same id keeps its cancel handle (audit F-24).
      calls.remove(id, call)
    }
  }

  fun cancel(id: String) {
    calls.remove(id)?.cancel()
  }
}
