package expo.modules.rfetransport

import java.io.BufferedReader
import java.io.InputStreamReader
import java.util.concurrent.ConcurrentHashMap
import java.util.concurrent.atomic.AtomicLong

/**
 * Pinned client of GET /v1/events (Server-Sent Events). [run] blocks on the connection and calls [onKind] with the
 * kind of each event until the stream ends or [stop] is called; it returns the HTTP status, 0 when the connection
 * failed (retry with backoff) or [PIN_MISMATCH] when the host's certificate did not match the pin (do not retry:
 * the host's identity changed). The agent sends a ping comment every 20 s, so a read timeout above that detects a dead link.
 */
object EventStream {
  private val calls = ConcurrentHashMap<String, okhttp3.Call>()
  /** Result of [run] when the certificate did not match the pin; no HTTP byte (or token) was sent. */
  const val PIN_MISMATCH = -1

  /** Counts stops. A [run] remembers the count when it begins and is cancelled only by a stop numbered after that. */
  private val stopClock = AtomicLong()
  /** The stop number of the latest [stop] that found no running call for an id. */
  private val stoppedAt = ConcurrentHashMap<String, Long>()

  /** Reads event lines from [lines]; calls [onKind] once per complete event. Comment lines and other fields are ignored. */
  fun parse(lines: Sequence<String>, onKind: (String) -> Unit) {
    var kind = ""
    for (raw in lines) {
      val line = raw.trimEnd('\r')
      when {
        line.isEmpty() -> {
          if (kind.isNotEmpty()) onKind(kind)
          kind = ""
        }
        line.startsWith("event:") -> kind = line.substring(6).trim()
      }
    }
  }

  fun run(id: String, url: String, headers: Map<String, String>, pin: String?, onKind: (String) -> Unit): Int {
    val began = stopClock.get()
    val call = PinnedHttp.newGetCall(url, headers + ("Accept" to "text/event-stream"), pin, 45000)
    calls[id] = call
    // A stop that landed between the line above and the registration finds no call to cancel; honour it here.
    if ((stoppedAt[id] ?: 0L) > began) call.cancel()
    try {
      call.execute().use { resp ->
        if (!resp.isSuccessful) return resp.code
        val reader = BufferedReader(InputStreamReader(resp.body!!.byteStream(), Charsets.UTF_8))
        parse(generateSequence { reader.readLine() }, onKind)
        return resp.code
      }
    } catch (e: java.io.IOException) {
      return if (PinnedHttp.isPinMismatch(e)) PIN_MISMATCH else 0
    } finally {
      calls.remove(id, call)
      stoppedAt.remove(id)
    }
  }

  fun stop(id: String) {
    val call = calls.remove(id)
    if (call != null) {
      call.cancel()
      return
    }
    if (stoppedAt.size > 256) stoppedAt.clear()
    stoppedAt[id] = stopClock.incrementAndGet()
  }
}
