package expo.modules.rfetransport

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.Service
import android.content.Context
import android.content.Intent
import android.content.pm.ServiceInfo
import android.os.Build
import android.os.IBinder
import androidx.core.app.NotificationCompat
import androidx.core.content.ContextCompat
import java.util.concurrent.atomic.AtomicInteger

/**
 * Foreground service that keeps the process alive while the [TransferEngine]
 * has active work, with an ongoing progress notification. It stops itself when
 * the engine goes idle. (Unlike the Flutter service, the work runs natively,
 * not in a UI isolate.)
 */
class TransferService : Service() {
  companion object {
    const val CHANNEL = "transfers"
    const val NOTIFICATION_ID = 0x5254

    @Volatile private var live: TransferService? = null

    // startForegroundService() calls whose onStartCommand has not run yet. stopSelf() discards such queued starts, and
    // the system then kills the app for a service that never reached startForeground(), so no stop while any is pending.
    private val pendingStarts = AtomicInteger(0)
    private val lock = Any()

    fun start(context: Context) {
      synchronized(lock) {
        pendingStarts.incrementAndGet()
        try {
          ContextCompat.startForegroundService(context, Intent(context, TransferService::class.java))
        } catch (t: IllegalStateException) {
          // Android 12+ refuses a foreground service started from the background (a scheduled photo backup). The
          // engine still runs while the process lives, and its journal resumes anything cut short, so this is not fatal.
          pendingStarts.decrementAndGet()
        } catch (t: Throwable) {
          pendingStarts.decrementAndGet()
          throw t
        }
      }
    }

    fun onEngineChange(context: Context, engine: TransferEngine?) {
      val e = engine ?: return
      val svc = live ?: return
      if (e.activeCount() == 0) {
        synchronized(lock) {
          if (pendingStarts.get() == 0 && e.activeCount() == 0) {
            svc.stopForeground(STOP_FOREGROUND_REMOVE)
            svc.stopSelf()
          }
        }
      } else {
        val running = e.list().filter { it.state == TransferState.RUNNING }
        val received = running.sumOf { it.received }
        val total = running.sumOf { if (it.total > 0) it.total else 0L }
        val pct = if (total > 0) ((received * 100) / total).toInt() else 0
        val mgr = context.getSystemService(Context.NOTIFICATION_SERVICE) as NotificationManager
        // Posted under the lock and only while work remains: outside it this could land after the service
        // removed its notification and leave an ongoing one nobody can dismiss (audit F-66).
        synchronized(lock) {
          if (e.activeCount() > 0) mgr.notify(NOTIFICATION_ID, svc.build("Transferring ${e.activeCount()} file(s)", pct))
        }
      }
    }
  }

  override fun onBind(intent: Intent?): IBinder? = null

  override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
    live = this
    ensureChannel()
    val n = build("Transferring…", 0)
    if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.UPSIDE_DOWN_CAKE) {
      startForeground(NOTIFICATION_ID, n, ServiceInfo.FOREGROUND_SERVICE_TYPE_DATA_SYNC)
    } else {
      startForeground(NOTIFICATION_ID, n)
    }
    synchronized(lock) {
      pendingStarts.updateAndGet { if (it > 0) it - 1 else 0 }
      // Nothing active (e.g. restarted after process death): do not linger.
      if (pendingStarts.get() == 0 && TransferHost.engine(this).activeCount() == 0) {
        stopForeground(STOP_FOREGROUND_REMOVE)
        stopSelf()
      }
    }
    return START_NOT_STICKY
  }

  /**
   * Android 15 caps a dataSync foreground service at about six hours in 24 and then calls this; a service that
   * does not stop gets the app killed. Pause the work (it resumes from its journal) and stop (audit F-65).
   */
  override fun onTimeout(startId: Int, fgsType: Int) {
    val engine = TransferHost.engine(this)
    engine.list().filter { it.state == TransferState.RUNNING || it.state == TransferState.QUEUED }.forEach { engine.pause(it.id) }
    stopForeground(STOP_FOREGROUND_REMOVE)
    // A start already requested must not be discarded by this stop (it would get the app killed); the pending
    // start re-enters the foreground itself. The check and the stop share the lock with start().
    synchronized(lock) {
      if (pendingStarts.get() == 0) stopSelf()
    }
  }

  override fun onDestroy() {
    live = null
    super.onDestroy()
  }

  internal fun build(title: String, progress: Int): Notification =
    NotificationCompat.Builder(this, CHANNEL)
      .setContentTitle(title)
      .setSmallIcon(android.R.drawable.stat_sys_download)
      .setOngoing(true)
      .setOnlyAlertOnce(true)
      .setProgress(100, progress.coerceIn(0, 100), progress <= 0)
      .setForegroundServiceBehavior(NotificationCompat.FOREGROUND_SERVICE_IMMEDIATE)
      .build()

  private fun ensureChannel() {
    if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
      val mgr = getSystemService(Context.NOTIFICATION_SERVICE) as NotificationManager
      if (mgr.getNotificationChannel(CHANNEL) == null) {
        mgr.createNotificationChannel(
          NotificationChannel(CHANNEL, "Transfers", NotificationManager.IMPORTANCE_LOW)
            .apply { description = "Ongoing file transfers" },
        )
      }
    }
  }
}
