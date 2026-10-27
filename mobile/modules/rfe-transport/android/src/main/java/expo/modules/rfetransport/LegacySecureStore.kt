package expo.modules.rfetransport

import android.content.Context
import com.it_nomads.fluttersecurestorage.FlutterSecureStorage
import com.it_nomads.fluttersecurestorage.FlutterSecureStorageConfig
import com.it_nomads.fluttersecurestorage.SecurePreferencesCallback
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit

/**
 * Reads and writes the SAME store the Flutter app used (flutter_secure_storage
 * 10.3.1, default options), by running the plugin's own vendored classes. Format
 * compatibility therefore does not depend on a re-implementation: the Keystore
 * alias, wrapped AES key, key prefix and cipher choice all come from upstream
 * code. Never used with new key names outside the `rfe_` namespace.
 */
class LegacySecureStore(context: Context) {
  private val storage = FlutterSecureStorage(context.applicationContext)
  private val config = FlutterSecureStorageConfig(HashMap<String, Any>())

  init {
    val latch = CountDownLatch(1)
    var failure: Exception? = null
    storage.initialize(config, object : SecurePreferencesCallback<Void> {
      override fun onSuccess(v: Void?) { latch.countDown() }
      override fun onError(e: Exception) { failure = e; latch.countDown() }
    })
    if (!latch.await(20, TimeUnit.SECONDS)) throw IllegalStateException("secure storage init timed out")
    failure?.let { throw it }
  }

  // The plugin's classes share one Cipher and are not thread-safe: parallel upload chunks each read the token, and
  // concurrent reads failed with "Cipher not initialized". Every access takes this lock.
  private val lock = Any()

  fun read(key: String): String? = synchronized(lock) {
    val k = storage.addPrefixToKey(key)
    if (storage.containsKey(k)) storage.read(k) else null
  }

  fun write(key: String, value: String) = synchronized(lock) { storage.write(storage.addPrefixToKey(key), value) }

  fun delete(key: String) = synchronized(lock) { storage.delete(storage.addPrefixToKey(key)) }

  /** Every entry, keys without the plugin's prefix. */
  fun readAll(): Map<String, String> = synchronized(lock) { storage.readAll() }

  fun contains(key: String): Boolean = synchronized(lock) { storage.containsKey(storage.addPrefixToKey(key)) }
}
