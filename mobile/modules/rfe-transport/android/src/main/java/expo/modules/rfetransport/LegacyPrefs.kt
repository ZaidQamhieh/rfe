package expo.modules.rfetransport

import android.content.Context
import org.json.JSONArray
import org.json.JSONObject

/**
 * Read-only view of the Flutter app's shared_preferences file
 * (`FlutterSharedPreferences`, keys prefixed `flutter.`). Used once for the
 * in-place upgrade import; the RN app then owns its own non-secret storage.
 * Lists are stored by the plugin as a marker prefix followed by a JSON array.
 * Values are returned JSON-encoded so JS can JSON.parse each one.
 */
object LegacyPrefs {
  private const val FILE = "FlutterSharedPreferences"
  private const val KEY_PREFIX = "flutter."
  private const val LIST_PREFIX = "VGhpcyBpcyB0aGUgcHJlZml4IGZvciBhIGxpc3Qu"

  fun readAll(context: Context): Map<String, String> {
    val prefs = context.applicationContext.getSharedPreferences(FILE, Context.MODE_PRIVATE)
    val out = HashMap<String, String>()
    for ((rawKey, value) in prefs.all) {
      if (!rawKey.startsWith(KEY_PREFIX)) continue
      out[rawKey.removePrefix(KEY_PREFIX)] = when (value) {
        is String ->
          // Observed on shared_preferences_android: marker + "!" + JSON array.
          if (value.startsWith(LIST_PREFIX)) JSONArray(value.removePrefix(LIST_PREFIX).removePrefix("!")).toString()
          else JSONObject.quote(value)
        is Set<*> -> JSONArray(value.toList()).toString()
        else -> value.toString() // Boolean, Long, Int, Float
      }
    }
    return out
  }
}
