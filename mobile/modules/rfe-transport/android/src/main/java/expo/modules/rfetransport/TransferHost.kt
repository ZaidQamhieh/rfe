package expo.modules.rfetransport

import android.content.Context
import java.io.File

/**
 * Process-wide owner of the [TransferEngine]. Lives outside any Activity or JS
 * runtime so downloads keep running (under [TransferService]) when the UI is
 * backgrounded. Secrets are read from the secure store per request.
 */
object TransferHost {
  @Volatile private var engine: TransferEngine? = null
  @Volatile var listener: ((TransferRecord) -> Unit)? = null

  fun engine(context: Context): TransferEngine = engine ?: synchronized(this) {
    engine ?: run {
      val app = context.applicationContext
      val store by lazy { LegacySecureStore(app) }
      val creds = object : Credentials {
        override fun pin(hostId: String) = store.read("rfe_fp_$hostId")
        override fun token(hostId: String) = store.read("rfe_token_$hostId")
      }
      TransferEngine(File(app.filesDir, "rfe-transfers"), creds, publisher = { PublicDownloads.publish(app, it) }, onChange = { r ->
        listener?.invoke(r)
        TransferService.onEngineChange(app, this.engine())
      }).also { engine = it }
    }
  }

  private fun engine(): TransferEngine? = engine

  fun ensureServiceRunning(context: Context) {
    TransferService.start(context.applicationContext)
  }
}
