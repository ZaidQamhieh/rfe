package expo.modules.rfetransport

import android.graphics.Bitmap
import android.graphics.Color
import android.graphics.pdf.PdfRenderer
import android.os.ParcelFileDescriptor
import java.io.File
import java.io.FileOutputStream

/**
 * Renders pages of a local PDF (already fetched through the pinned client) to PNG files using the
 * platform PdfRenderer. PdfRenderer allows one open page at a time and is not thread-safe, so every
 * call is serialized and the most recently used document stays open for the next page.
 */
object PdfPages {
  private var openPath: String? = null
  private var fd: ParcelFileDescriptor? = null
  private var renderer: PdfRenderer? = null

  @Synchronized
  private fun open(path: String): PdfRenderer {
    renderer?.let { if (openPath == path) return it }
    closeLocked()
    val d = ParcelFileDescriptor.open(File(path), ParcelFileDescriptor.MODE_READ_ONLY)
    try {
      renderer = PdfRenderer(d)
    } catch (e: Exception) {
      d.close()
      throw e
    }
    fd = d
    openPath = path
    return renderer!!
  }

  private fun closeLocked() {
    try { renderer?.close() } catch (_: Exception) {}
    try { fd?.close() } catch (_: Exception) {}
    renderer = null
    fd = null
    openPath = null
  }

  @Synchronized
  fun close() = closeLocked()

  @Synchronized
  fun pageCount(path: String): Int = open(path).pageCount

  /**
   * Renders page [index] at [widthPx] wide (height keeps the page's aspect ratio, capped so a
   * pathological page can't allocate an enormous bitmap) on white, writes a PNG to [outPath]
   * atomically, and returns the pixel size.
   */
  @Synchronized
  fun render(path: String, index: Int, widthPx: Int, outPath: String): Map<String, Int> {
    val r = open(path)
    require(index in 0 until r.pageCount) { "page $index out of range" }
    r.openPage(index).use { page ->
      val ratio = page.height.toDouble() / page.width.coerceAtLeast(1)
      var w = widthPx.coerceIn(64, 2048)
      var h = (w * ratio).toInt().coerceAtLeast(1)
      // A very tall page is scaled down as a whole; clamping only the height would stretch it (audit F-50).
      if (h > 4096) {
        h = 4096
        w = (4096 / ratio).toInt().coerceAtLeast(1)
      }
      val bmp = Bitmap.createBitmap(w, h, Bitmap.Config.ARGB_8888)
      try {
        bmp.eraseColor(Color.WHITE)
        page.render(bmp, null, null, PdfRenderer.Page.RENDER_MODE_FOR_DISPLAY)
        val out = File(outPath)
        out.parentFile?.mkdirs()
        val part = File("$outPath.part")
        FileOutputStream(part).use { bmp.compress(Bitmap.CompressFormat.PNG, 100, it) }
        if (!part.renameTo(out)) throw java.io.IOException("could not publish page image")
        return mapOf("width" to w, "height" to h)
      } finally {
        bmp.recycle()
      }
    }
  }
}
