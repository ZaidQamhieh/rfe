package expo.modules.rfetransport

import okhttp3.Call
import okhttp3.OkHttpClient
import okhttp3.Request
import java.io.File
import java.io.IOException
import java.io.RandomAccessFile
import java.util.concurrent.ConcurrentHashMap
import java.util.concurrent.TimeUnit

/** A resumed download was answered with a full body, so the partial file no longer matches. */
class RangeNotHonoured : IOException("The server ignored the resume request.")

/**
 * Plain public HTTPS downloads with the platform's normal TLS trust, for things that are not our own agent (the app
 * update on GitHub Releases). Agent traffic never goes through here: it stays on the pinned client. Resumes with an
 * HTTP Range from the size already on disk and appends; a server that answers a ranged request with a full 200 makes
 * the partial file invalid, so it is deleted and [RangeNotHonoured] is thrown.
 */
class PublicHttp(private val client: OkHttpClient = OkHttpClient.Builder().readTimeout(30, TimeUnit.SECONDS).followSslRedirects(false).build(), private val allowCleartext: Boolean = false) {
  private val calls = ConcurrentHashMap<String, Call>()

  /** Returns the file's size when the body is complete. */
  fun download(id: String, url: String, dest: File, offset: Long): Long {
    if (!allowCleartext && !url.startsWith("https://")) throw IOException("Only https downloads are allowed.")
    dest.parentFile?.mkdirs()
    val req = Request.Builder().url(url).apply { if (offset > 0) header("Range", "bytes=$offset-") }.build()
    val call = client.newCall(req)
    calls[id] = call
    try {
      call.execute().use { res ->
        // 416: the partial no longer fits the file on the server. Any other failure keeps it for a later try.
        if (offset > 0 && res.code == 416) {
          dest.delete()
          throw RangeNotHonoured()
        }
        if (!res.isSuccessful) throw IOException("HTTP ${res.code}")
        if (offset > 0 && res.code != 206) {
          dest.delete()
          throw RangeNotHonoured()
        }
        val body = res.body ?: throw IOException("Empty response")
        RandomAccessFile(dest, "rw").use { out ->
          if (offset > 0) out.seek(offset) else out.setLength(0)
          body.byteStream().use { input ->
            val buf = ByteArray(64 * 1024)
            while (true) {
              val n = input.read(buf)
              if (n < 0) break
              out.write(buf, 0, n)
            }
          }
        }
      }
    } finally {
      calls.remove(id)
    }
    return dest.length()
  }

  fun cancel(id: String) {
    calls[id]?.cancel()
  }
}

/** Lower-case hex SHA-256 of a file, streamed. */
fun sha256Hex(f: File): String = f.inputStream().use { sha256Hex(it) }

/** Lower-case hex SHA-256 of a stream (a published download is read through its content URI). */
fun sha256Hex(input: java.io.InputStream): String {
  val md = java.security.MessageDigest.getInstance("SHA-256")
  val buf = ByteArray(64 * 1024)
  while (true) {
    val n = input.read(buf)
    if (n < 0) break
    md.update(buf, 0, n)
  }
  return md.digest().joinToString("") { "%02x".format(it) }
}
