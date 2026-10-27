package expo.modules.rfetransport

import android.content.ActivityNotFoundException
import android.content.Context
import android.content.Intent
import android.net.Uri
import android.os.Build
import android.provider.Settings
import androidx.core.content.FileProvider
import java.io.File

/**
 * Hands a local file to another app through a FileProvider content URI, so the receiver gets read access to that
 * one file without any storage permission. Only files under the paths declared in `rfe_file_paths.xml` (the app's
 * cache/share and cache/open folders) can be exposed.
 */
object ExternalFiles {
  fun authority(ctx: Context) = ctx.packageName + ".rfe.fileprovider"

  private fun uri(ctx: Context, file: File) = FileProvider.getUriForFile(ctx, authority(ctx), file)

  /** ACTION_VIEW for [mime]; false when no installed app can open it. */
  fun open(ctx: Context, file: File, mime: String): Boolean {
    val intent = Intent(Intent.ACTION_VIEW).setDataAndType(uri(ctx, file), mime)
      .addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION or Intent.FLAG_ACTIVITY_NEW_TASK)
    return try { ctx.startActivity(intent); true } catch (_: ActivityNotFoundException) { false }
  }

  /** The system share sheet for one file. */
  fun share(ctx: Context, file: File, mime: String): Boolean {
    val send = Intent(Intent.ACTION_SEND).setType(mime).putExtra(Intent.EXTRA_STREAM, uri(ctx, file))
      .addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION)
    val chooser = Intent.createChooser(send, null).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
    return try { ctx.startActivity(chooser); true } catch (_: ActivityNotFoundException) { false }
  }

  private const val APK_MIME = "application/vnd.android.package-archive"

  /** True when this app may hand an APK to the installer (the "install unknown apps" switch for this app). */
  fun canInstall(ctx: Context): Boolean = Build.VERSION.SDK_INT < Build.VERSION_CODES.O || ctx.packageManager.canRequestPackageInstalls()

  /** Opens this app's "install unknown apps" settings page. */
  fun openInstallSettings(ctx: Context): Boolean {
    val intent = Intent(Settings.ACTION_MANAGE_UNKNOWN_APP_SOURCES, Uri.parse("package:" + ctx.packageName)).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
    return try { ctx.startActivity(intent); true } catch (_: ActivityNotFoundException) { false }
  }

  /** Hands a downloaded APK (under cache/updates) to the system installer; the system asks the user to confirm. */
  fun installApk(ctx: Context, file: File): Boolean = open(ctx, file, APK_MIME)
}
