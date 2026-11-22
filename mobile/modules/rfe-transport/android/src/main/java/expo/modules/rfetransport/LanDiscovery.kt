package expo.modules.rfetransport

import android.content.Context
import android.net.nsd.NsdManager
import android.net.nsd.NsdServiceInfo
import android.net.wifi.WifiManager
import android.os.Handler
import android.os.Looper
import java.net.Inet4Address

/**
 * mDNS/DNS-SD search for `_rfe._tcp.` agents, ported from the Flutter
 * MainActivity: IPv4 only, at most 64 candidates, multicast lock held during the
 * search. Each agent is reported as soon as it resolves; the search ends after
 * TIMEOUT_MS, or QUIET_MS after the last agent was found. Discovery results are UNTRUSTED hints: pairing still
 * requires an out-of-band certificate fingerprint.
 */
class LanDiscovery(context: Context) {
  private val app = context.applicationContext
  private val handler = Handler(Looper.getMainLooper())
  private val lock = Any()
  private val candidates = linkedMapOf<String, Map<String, Any>>()
  private val resolving = mutableSetOf<String>()
  private var manager: NsdManager? = null
  private var listener: NsdManager.DiscoveryListener? = null
  private var multicast: WifiManager.MulticastLock? = null
  private var timeout: Runnable? = null
  private var quiet: Runnable? = null
  private var done: ((List<Map<String, Any>>, String?) -> Unit)? = null
  private var active = false

  companion object {
    const val SERVICE_TYPE = "_rfe._tcp."
    /**
     * Upper bound of one search. mDNS queries are re-sent after about 1 s and 3 s, so a computer that missed the first
     * one answers well inside 5 s; the old 8 s only made an empty or finished search feel slow.
     */
    const val TIMEOUT_MS = 5_000L

    /** Once something was found, how long to wait for another before ending (longer than the 2 s retransmission gap). */
    const val QUIET_MS = 2_000L
    const val MAX_CANDIDATES = 64
  }

  /** Starts a search; [onFound] gets each agent as it resolves (main thread), [callback] gets (agents, errorCode). Only one search may run. */
  fun scan(onFound: (Map<String, Any>) -> Unit, callback: (List<Map<String, Any>>, String?) -> Unit) {
    if (done != null) return callback(emptyList(), "SCAN_BUSY")
    val mgr = app.getSystemService(Context.NSD_SERVICE) as? NsdManager
      ?: return callback(emptyList(), "DISCOVERY_UNAVAILABLE")
    manager = mgr
    done = callback
    synchronized(lock) {
      active = true
      candidates.clear()
      resolving.clear()
    }
    val l = object : NsdManager.DiscoveryListener {
      override fun onDiscoveryStarted(serviceType: String) = Unit
      override fun onServiceLost(serviceInfo: NsdServiceInfo) = Unit
      override fun onDiscoveryStopped(serviceType: String) = Unit
      override fun onStartDiscoveryFailed(serviceType: String, errorCode: Int) = finish("DISCOVERY_FAILED")
      override fun onStopDiscoveryFailed(serviceType: String, errorCode: Int) = finish("DISCOVERY_FAILED")

      override fun onServiceFound(serviceInfo: NsdServiceInfo) {
        if (!serviceInfo.serviceType.startsWith("_rfe._tcp")) return
        val key = "${serviceInfo.serviceName}|${serviceInfo.serviceType}"
        val go = synchronized(lock) { active && candidates.size < MAX_CANDIDATES && resolving.add(key) }
        if (!go) return
        try {
          @Suppress("DEPRECATION")
          mgr.resolveService(serviceInfo, object : NsdManager.ResolveListener {
            override fun onResolveFailed(info: NsdServiceInfo, errorCode: Int) {
              synchronized(lock) { resolving.remove(key) }
            }

            override fun onServiceResolved(info: NsdServiceInfo) {
              synchronized(lock) { resolving.remove(key) }
              @Suppress("DEPRECATION")
              val addr = info.host as? Inet4Address ?: return
              val host = addr.hostAddress ?: return
              if (info.port !in 1..65535) return
              val name = info.serviceName.trim().ifEmpty { "RFE computer" }
              val agent = mapOf("name" to name.take(128), "address" to host, "port" to info.port)
              val fresh = synchronized(lock) { active && candidates.put("$host:${info.port}", agent) == null }
              if (fresh) handler.post {
                if (done != null) {
                  onFound(agent)
                  armQuiet()
                }
              }
            }
          })
        } catch (_: RuntimeException) {
          synchronized(lock) { resolving.remove(key) }
        }
      }
    }
    listener = l
    try {
      val wifi = app.getSystemService(Context.WIFI_SERVICE) as? WifiManager
      multicast = wifi?.createMulticastLock("rfe-lan-discovery")?.apply {
        setReferenceCounted(false)
        acquire()
      }
      val t = Runnable { finish(null) }
      timeout = t
      handler.postDelayed(t, TIMEOUT_MS)
      mgr.discoverServices(SERVICE_TYPE, NsdManager.PROTOCOL_DNS_SD, l)
    } catch (e: RuntimeException) {
      finish("DISCOVERY_FAILED")
    }
  }

  fun stop() = finish(null)

  /** (Re)starts the quiet period that ends the search when nothing more is found. */
  private fun armQuiet() {
    quiet?.let(handler::removeCallbacks)
    val q = Runnable { finish(null) }
    quiet = q
    handler.postDelayed(q, QUIET_MS)
  }

  private fun finish(error: String?) {
    timeout?.let(handler::removeCallbacks)
    timeout = null
    quiet?.let(handler::removeCallbacks)
    quiet = null
    multicast?.let { if (it.isHeld) it.release() }
    multicast = null
    listener?.let { l -> try { manager?.stopServiceDiscovery(l) } catch (_: RuntimeException) {} }
    listener = null
    val cb = done
    done = null
    val found = synchronized(lock) {
      active = false
      resolving.clear()
      candidates.values.toList()
    }
    manager = null
    cb?.invoke(if (error == null) found else emptyList(), error)
  }
}
