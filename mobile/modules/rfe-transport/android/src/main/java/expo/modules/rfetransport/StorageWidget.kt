package expo.modules.rfetransport

import android.app.PendingIntent
import android.appwidget.AppWidgetManager
import android.appwidget.AppWidgetProvider
import android.content.ComponentName
import android.content.Context
import android.widget.RemoteViews

/**
 * Home-screen storage widget: the computer's name, a used-space bar and "x free of y". The app writes the numbers
 * ([save]) whenever it reads a computer's drives; the widget itself makes no network calls, so it holds no token or pin
 * and shows the last known values. Tapping it opens the app.
 */
class StorageWidget : AppWidgetProvider() {
  override fun onUpdate(context: Context, manager: AppWidgetManager, ids: IntArray) {
    for (id in ids) manager.updateAppWidget(id, views(context))
  }

  companion object {
    private const val PREFS = "rfe_storage_widget"

    private fun views(ctx: Context): RemoteViews {
      val p = ctx.getSharedPreferences(PREFS, Context.MODE_PRIVATE)
      val v = RemoteViews(ctx.packageName, R.layout.rfe_storage_widget)
      val has = p.contains("title")
      v.setTextViewText(R.id.rfe_widget_title, if (has) p.getString("title", "") else ctx.getString(R.string.rfe_widget_empty))
      v.setTextViewText(R.id.rfe_widget_detail, if (has) p.getString("detail", "") else "")
      v.setProgressBar(R.id.rfe_widget_bar, 100, if (has) p.getInt("percent", 0).coerceIn(0, 100) else 0, false)
      ctx.packageManager.getLaunchIntentForPackage(ctx.packageName)?.let {
        val pi = PendingIntent.getActivity(ctx, 0, it, PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT)
        v.setOnClickPendingIntent(R.id.rfe_widget_root, pi)
      }
      return v
    }

    /** Stores the latest numbers and redraws every placed widget. */
    fun save(ctx: Context, title: String, detail: String, percent: Int) {
      ctx.getSharedPreferences(PREFS, Context.MODE_PRIVATE).edit()
        .putString("title", title).putString("detail", detail).putInt("percent", percent).apply()
      val mgr = AppWidgetManager.getInstance(ctx)
      val ids = mgr.getAppWidgetIds(ComponentName(ctx, StorageWidget::class.java))
      for (id in ids) mgr.updateAppWidget(id, views(ctx))
    }
  }
}
