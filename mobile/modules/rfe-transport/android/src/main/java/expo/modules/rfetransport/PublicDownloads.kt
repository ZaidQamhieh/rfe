package expo.modules.rfetransport

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

  /** How many different names are tried before giving up and leaving the file in app storage. */
  private const val MAX_NAME_TRIES = 20

  fun mimeFor(name: String): String {
    val ext = name.substringAfterLast('.', "").lowercase()
    return MimeTypeMap.getSingleton().getMimeTypeFromExtension(ext) ?: "application/octet-stream"
  }

  /** The file names already in [FOLDER] that this app can see; empty when the query fails. */
  private fun namesInFolder(ctx: Context): Set<String> = runCatching {
    val names = HashSet<String>()
    ctx.contentResolver.query(
      MediaStore.Downloads.EXTERNAL_CONTENT_URI, arrayOf(MediaStore.MediaColumns.DISPLAY_NAME),
      "${MediaStore.MediaColumns.RELATIVE_PATH} = ?", arrayOf("$FOLDER/"), null,
    )?.use { c -> while (c.moveToNext()) c.getString(0)?.let(names::add) }
    names
  }.getOrDefault(emptySet())

  /**
   * Copies [src] into Downloads and returns the content URI, or null when publishing is unavailable or failed.
   * A name that is already taken (an earlier download, or a file this app cannot see) is skipped for the next numbered
   * one ("g1 (1).bin"); a name MediaStore refuses to create is retried under the next number instead of giving up.
   */
  fun publish(ctx: Context, src: File): String? {
    if (Build.VERSION.SDK_INT < Build.VERSION_CODES.Q) return null
    val resolver = ctx.contentResolver
    val taken = HashSet(namesInFolder(ctx))
    repeat(MAX_NAME_TRIES) {
      val name = DownloadNames.firstFree(src.name, taken)
      taken.add(name)
      val values = ContentValues().apply {
        put(MediaStore.MediaColumns.DISPLAY_NAME, name)
        put(MediaStore.MediaColumns.MIME_TYPE, mimeFor(src.name))
        put(MediaStore.MediaColumns.RELATIVE_PATH, FOLDER)
        put(MediaStore.MediaColumns.IS_PENDING, 1)
      }
      val uri = runCatching { resolver.insert(MediaStore.Downloads.EXTERNAL_CONTENT_URI, values) }.getOrNull() ?: return@repeat
      val out = runCatching { resolver.openOutputStream(uri) }.getOrNull()
      if (out == null) {
        runCatching { resolver.delete(uri, null, null) }
        return@repeat
      }
      return try {
        out.use { o -> src.inputStream().use { it.copyTo(o, 64 * 1024) } }
        resolver.update(uri, ContentValues().apply { put(MediaStore.MediaColumns.IS_PENDING, 0) }, null, null)
        uri.toString()
      } catch (_: Exception) {
        runCatching { resolver.delete(uri, null, null) }
        null
      }
    }
    return null
  }

  /** The name MediaStore saved [uri] under (it renames a duplicate), or null when it cannot be read. */
  fun displayName(ctx: Context, uri: String): String? =
    ctx.contentResolver.query(Uri.parse(uri), arrayOf(MediaStore.MediaColumns.DISPLAY_NAME), null, null, null)?.use { c ->
      if (c.moveToFirst()) c.getString(0) else null
    }

  /** ACTION_VIEW on a MediaStore URI this app published; anything else is refused. */
  fun open(ctx: Context, uri: String, mime: String): Boolean {
    val parsed = Uri.parse(uri)
    // Only the Downloads collection this app publishes into, not any MediaStore item (audit F-79).
    if (parsed.scheme != "content" || parsed.authority != MediaStore.AUTHORITY || !parsed.pathSegments.contains("downloads")) return false
    val intent = Intent(Intent.ACTION_VIEW).setDataAndType(parsed, mime)
      .addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION or Intent.FLAG_ACTIVITY_NEW_TASK)
    return ExternalFiles.startActivitySafely(ctx, intent)
  }
}
