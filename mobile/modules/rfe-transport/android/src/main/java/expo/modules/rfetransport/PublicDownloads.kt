package expo.modules.rfetransport

import android.content.ActivityNotFoundException
import android.content.ContentValues
import android.content.Context
import android.content.Intent
import android.net.Uri
import android.os.Build
import android.provider.MediaStore
import android.webkit.MimeTypeMap
import java.io.File

/**
 * Publishes finished downloads into the shared Downloads collection (`Download/RFE`) through
 * MediaStore, so other apps and the system Files app can see them without any storage permission. Android 10 and
 * newer only; older releases keep the file in app storage.
 */
object PublicDownloads {
  const val FOLDER = "Download/RFE"

  fun mimeFor(name: String): String {
    val ext = name.substringAfterLast('.', "").lowercase()
    return MimeTypeMap.getSingleton().getMimeTypeFromExtension(ext) ?: "application/octet-stream"
  }

  /** Copies [src] into Downloads and returns the content URI, or null when publishing is unavailable or failed. */
  fun publish(ctx: Context, src: File): String? {
    if (Build.VERSION.SDK_INT < Build.VERSION_CODES.Q) return null
    val resolver = ctx.contentResolver
    val values = ContentValues().apply {
      put(MediaStore.MediaColumns.DISPLAY_NAME, src.name)
      put(MediaStore.MediaColumns.MIME_TYPE, mimeFor(src.name))
      put(MediaStore.MediaColumns.RELATIVE_PATH, FOLDER)
      put(MediaStore.MediaColumns.IS_PENDING, 1)
    }
    val uri = runCatching { resolver.insert(MediaStore.Downloads.EXTERNAL_CONTENT_URI, values) }.getOrNull() ?: return null
    return try {
      resolver.openOutputStream(uri)!!.use { out -> src.inputStream().use { it.copyTo(out, 64 * 1024) } }
      resolver.update(uri, ContentValues().apply { put(MediaStore.MediaColumns.IS_PENDING, 0) }, null, null)
      uri.toString()
    } catch (_: Exception) {
      runCatching { resolver.delete(uri, null, null) }
      null
    }
  }

  /** ACTION_VIEW on a MediaStore URI this app published; anything else is refused. */
  fun open(ctx: Context, uri: String, mime: String): Boolean {
    val parsed = Uri.parse(uri)
    // Only the Downloads collection this app publishes into, not any MediaStore item (audit F-79).
    if (parsed.scheme != "content" || parsed.authority != MediaStore.AUTHORITY || !parsed.pathSegments.contains("downloads")) return false
    val intent = Intent(Intent.ACTION_VIEW).setDataAndType(parsed, mime)
      .addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION or Intent.FLAG_ACTIVITY_NEW_TASK)
    return try { ctx.startActivity(intent); true } catch (_: ActivityNotFoundException) { false }
  }
}
