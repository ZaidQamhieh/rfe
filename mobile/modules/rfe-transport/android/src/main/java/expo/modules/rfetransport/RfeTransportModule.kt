package expo.modules.rfetransport

import android.content.Context
import android.net.ConnectivityManager
import android.net.NetworkCapabilities
import android.util.Base64
import expo.modules.kotlin.exception.CodedException
import expo.modules.kotlin.modules.Module
import expo.modules.kotlin.functions.Coroutine
import expo.modules.kotlin.modules.ModuleDefinition
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import java.io.File

class RfeTransportModule : Module() {
  private var discovery: LanDiscovery? = null
  private val secure by lazy {
    LegacySecureStore(appContext.reactContext ?: throw CodedException("ERR_NO_CONTEXT", "no context", null))
  }

  private fun <T> guard(block: () -> T): T = try {
    block()
  } catch (e: FetchTooLarge) {
    throw CodedException("ERR_TOO_LARGE", e.message, e)
  } catch (e: CertPinMismatch) {
    throw CodedException("ERR_CERT_PIN_MISMATCH", e.message, e)
  } catch (e: PinPolicyViolation) {
    throw CodedException("ERR_PIN_POLICY", e.message, e)
  } catch (e: javax.net.ssl.SSLException) {
    // Any TLS failure is fatal for this route; a pin mismatch surfaces as
    // CertPinMismatch (possibly wrapped) and must never look like "unreachable".
    val mismatch = generateSequence<Throwable>(e) { it.cause }.any { it is CertPinMismatch }
    if (mismatch) throw CodedException("ERR_CERT_PIN_MISMATCH", e.message, e)
    throw CodedException("ERR_TLS", e.message, e)
  } catch (e: java.io.IOException) {
    // A pin mismatch wrapped in a plain IOException is still a mismatch, not "unreachable" (audit F-153).
    val mismatch = generateSequence<Throwable>(e) { it.cause }.any { it is CertPinMismatch }
    if (mismatch) throw CodedException("ERR_CERT_PIN_MISMATCH", e.message, e)
    throw CodedException("ERR_CONNECTION", e.message, e)
  }

  private val publicHttp = PublicHttp()

  private fun <T> backup(block: () -> T): T = try {
    block()
  } catch (e: BackupCryptoException) {
    throw CodedException("ERR_BACKUP", e.message, e)
  }

  private fun <T> pdf(block: () -> T): T = try {
    block()
  } catch (e: SecurityException) {
    throw CodedException("ERR_PDF", "This PDF is password-protected.", e)
  } catch (e: Exception) {
    throw CodedException("ERR_PDF", e.message ?: "Could not render this PDF.", e)
  }

  // Plain AsyncFunction bodies share Expo's single "AsyncFunctionQueue" thread, so one slow blocking
  // call (a ping to an offline host) would stall every other native call. Network work runs on IO.
  private suspend fun <T> io(block: () -> T): T = withContext(Dispatchers.IO) { guard(block) }

  private val offlineKeyLock = Any()

  // Bodies of pinned folders, encrypted at rest. The key is created once and kept in secure storage.
  private val bodies by lazy {
    val ctx = appContext.reactContext ?: throw CodedException("ERR_NO_CONTEXT", "no context", null)
    OfflineBodyStore(File(ctx.filesDir, "offline_cache")) {
      val name = "rfe.offline_body_cache.aes_gcm_hkdf.v1"
      // One creator at a time, re-reading inside the lock: two first uses must not each make a key and
      // let the last write win, which would orphan the bodies stored under the other (audit F-109).
      synchronized(offlineKeyLock) {
        val existing = secure.read(name)?.let { Base64.decode(it, Base64.NO_WRAP) }
        if (existing != null && existing.size == OfflineBodyStore.KEY_BYTES) existing
        else OfflineBodyStore.newKey().also { secure.write(name, Base64.encodeToString(it, Base64.NO_WRAP)) }
      }
    }
  }

  private fun <T> bodies(block: (OfflineBodyStore) -> T): T = try {
    block(bodies)
  } catch (e: OfflineBodyIntegrityException) {
    throw CodedException("ERR_INTEGRITY", e.message, e)
  }

  override fun definition() = ModuleDefinition {
    Name("RfeTransport")
    Events("onTransferUpdate")

    OnCreate {
      TransferHost.listener = { r -> sendEvent("onTransferUpdate", mapOf("record" to r.toJson().toString())) }
    }
    OnDestroy { TransferHost.listener = null }

    // Transports of the active network (wifi, ethernet, cellular, vpn, bluetooth); empty when offline.
    AsyncFunction("networkTransports") {
      val ctx = appContext.reactContext ?: throw CodedException("ERR_NO_CONTEXT", "no context", null)
      networkTransports(ctx)
    }

    AsyncFunction("offlineBodyPut") Coroutine { hostId: String, path: String, srcPath: String -> withContext(Dispatchers.IO) { bodies { it.put(hostId, path, File(srcPath)) } } }
    AsyncFunction("offlineBodyRestore") Coroutine { hostId: String, path: String, destPath: String -> withContext(Dispatchers.IO) { bodies { it.restore(hostId, path, File(destPath)) } } }
    AsyncFunction("offlineBodyHas") Coroutine { hostId: String, path: String -> withContext(Dispatchers.IO) { bodies { it.has(hostId, path) } } }
    AsyncFunction("offlineBodyTotalBytes") Coroutine { -> withContext(Dispatchers.IO) { bodies { it.totalBytes().toDouble() } } }
    AsyncFunction("offlineBodyRemove") Coroutine { hostId: String, path: String -> withContext(Dispatchers.IO) { bodies { it.remove(hostId, path) } } }
    AsyncFunction("offlineBodyEvictHost") Coroutine { hostId: String -> withContext(Dispatchers.IO) { bodies { it.evictHost(hostId) } } }

    // Open or share a local file with another app (FileProvider URI, read-only, one file).
    AsyncFunction("openFileExternal") { path: String, mime: String ->
      val ctx = appContext.reactContext ?: throw CodedException("ERR_NO_CONTEXT", "no context", null)
      ExternalFiles.open(ctx, File(path), mime)
    }
    AsyncFunction("openPublicUri") { uri: String, mime: String ->
      val ctx = appContext.reactContext ?: throw CodedException("ERR_NO_CONTEXT", "no context", null)
      PublicDownloads.open(ctx, uri, mime)
    }
    AsyncFunction("shareFileExternal") { path: String, mime: String ->
      val ctx = appContext.reactContext ?: throw CodedException("ERR_NO_CONTEXT", "no context", null)
      ExternalFiles.share(ctx, File(path), mime)
    }

    AsyncFunction("transferEnqueue") { id: String, hostId: String, address: String, remotePath: String, destPath: String ->
      val ctx = appContext.reactContext ?: throw CodedException("ERR_NO_CONTEXT", "no context", null)
      val record = TransferHost.engine(ctx).enqueue(id, hostId, address, remotePath, destPath)
      TransferHost.ensureServiceRunning(ctx)
      record.toJson().toString()
    }
    AsyncFunction("transferEnqueueUpload") { id: String, hostId: String, address: String, localPath: String, remotePath: String, overwrite: Boolean, deleteSource: Boolean ->
      val ctx = appContext.reactContext ?: throw CodedException("ERR_NO_CONTEXT", "no context", null)
      val record = TransferHost.engine(ctx).enqueueUpload(id, hostId, address, localPath, remotePath, overwrite, deleteSource)
      TransferHost.ensureServiceRunning(ctx)
      record.toJson().toString()
    }
    AsyncFunction("transferResume") { id: String ->
      val ctx = appContext.reactContext ?: throw CodedException("ERR_NO_CONTEXT", "no context", null)
      TransferHost.engine(ctx).resume(id)
      TransferHost.ensureServiceRunning(ctx)
    }
    AsyncFunction("transferPause") { id: String ->
      TransferHost.engine(appContext.reactContext ?: throw CodedException("ERR_NO_CONTEXT", "no context", null)).pause(id)
    }
    AsyncFunction("transferCancel") { id: String ->
      TransferHost.engine(appContext.reactContext ?: throw CodedException("ERR_NO_CONTEXT", "no context", null)).cancel(id)
    }
    AsyncFunction("transferRemove") { id: String ->
      TransferHost.engine(appContext.reactContext ?: throw CodedException("ERR_NO_CONTEXT", "no context", null)).remove(id)
    }
    AsyncFunction("transfersList") {
      TransferHost.engine(appContext.reactContext ?: throw CodedException("ERR_NO_CONTEXT", "no context", null))
        .list().map { it.toJson().toString() }
    }

    AsyncFunction("fetchToFile") Coroutine { id: String, url: String, headers: Map<String, String>, pin: String?, destPath: String, timeoutMs: Double?, maxBytes: Double? ->
      io {
        val r = FetchFile.get(id, url, headers, pin, File(destPath), (timeoutMs ?: 20000.0).toLong(), (maxBytes ?: 0.0).toLong())
        mapOf("status" to r.status, "retryAfter" to r.retryAfterSeconds, "bytes" to r.bytes.toDouble())
      }
    }
    AsyncFunction("fetchCancel") { id: String -> FetchFile.cancel(id) }

    // A corrupt or password-protected PDF surfaces as ERR_PDF instead of a crash.
    AsyncFunction("pdfPageCount") Coroutine { path: String ->
      withContext(Dispatchers.IO) { pdf { PdfPages.pageCount(path) } }
    }
    AsyncFunction("pdfRenderPage") Coroutine { path: String, index: Int, widthPx: Int, outPath: String ->
      withContext(Dispatchers.IO) { pdf { PdfPages.render(path, index, widthPx, outPath) } }
    }
    AsyncFunction("pdfClose") { PdfPages.close() }

    AsyncFunction("mediaProxyStart") Coroutine { id: String, url: String, headers: Map<String, String>, pin: String? ->
      io { MediaProxy.start(id, url, headers, pin) }
    }
    AsyncFunction("mediaProxyStop") { id: String -> MediaProxy.stop(id) }

    AsyncFunction("sendWakeOnLan") Coroutine { mac: String -> io { Wol.send(mac) } }

    AsyncFunction("deviceId") {
      val ctx = appContext.reactContext ?: throw CodedException("ERR_NO_CONTEXT", "no context", null)
      // Settings.Secure.ANDROID_ID: stable per device+signing key; lets a re-pair reuse the same device row.
      android.provider.Settings.Secure.getString(ctx.contentResolver, android.provider.Settings.Secure.ANDROID_ID)
    }

    AsyncFunction("discoveryScan") { promise: expo.modules.kotlin.Promise ->
      val ctx = appContext.reactContext ?: return@AsyncFunction promise.reject(CodedException("ERR_NO_CONTEXT", "no context", null))
      val d = discovery ?: LanDiscovery(ctx).also { discovery = it }
      d.scan { agents, err ->
        if (err != null) promise.reject(CodedException(err, "Local network search failed", null)) else promise.resolve(agents)
      }
    }
    AsyncFunction("discoveryStop") { discovery?.stop() }

    AsyncFunction("legacyPrefsReadAll") {
      LegacyPrefs.readAll(appContext.reactContext ?: throw CodedException("ERR_NO_CONTEXT", "no context", null))
    }

    AsyncFunction("secureRead") { key: String -> secure.read(key) }
    AsyncFunction("secureWrite") { key: String, value: String -> secure.write(key, value) }
    AsyncFunction("secureDelete") { key: String -> secure.delete(key) }
    AsyncFunction("secureContains") { key: String -> secure.contains(key) }
    AsyncFunction("secureReadAll") Coroutine { -> withContext(Dispatchers.IO) { secure.readAll() } }

    // Encrypted settings backup (format shared with the Flutter app); PBKDF2 at 200k rounds is too slow for the JS thread.
    AsyncFunction("backupEncrypt") Coroutine { payload: String, passphrase: String -> withContext(Dispatchers.Default) { backup { BackupCrypto.encrypt(payload, passphrase) } } }
    AsyncFunction("backupDecrypt") Coroutine { envelope: String, passphrase: String -> withContext(Dispatchers.Default) { backup { BackupCrypto.decrypt(envelope, passphrase) } } }

    // App updates: this build's versionCode, a plain-HTTPS resumable download, the SHA-256 of a file, and the installer hand-off.
    AsyncFunction("appBuild") {
      val ctx = appContext.reactContext ?: throw CodedException("ERR_NO_CONTEXT", "no context", null)
      androidx.core.content.pm.PackageInfoCompat.getLongVersionCode(ctx.packageManager.getPackageInfo(ctx.packageName, 0)).toDouble()
    }
    AsyncFunction("publicDownload") Coroutine { id: String, url: String, destPath: String, offset: Double ->
      withContext(Dispatchers.IO) {
        try {
          publicHttp.download(id, url, File(destPath), offset.toLong()).toDouble()
        } catch (e: RangeNotHonoured) {
          throw CodedException("ERR_RANGE", e.message, e)
        } catch (e: java.io.IOException) {
          // OkHttp reports a call cancelled by publicDownloadCancel as IOException("Canceled"): the user's own
          // cancel, not a connection problem.
          if (e.message == "Canceled") throw CodedException("ERR_CANCELLED", "cancelled", e)
          throw CodedException("ERR_CONNECTION", e.message, e)
        }
      }
    }
    AsyncFunction("publicDownloadCancel") { id: String -> publicHttp.cancel(id) }
    AsyncFunction("sha256File") Coroutine { path: String ->
      withContext(Dispatchers.IO) {
        // A download published to Downloads no longer has its app-private file: hash it through its content URI.
        if (path.startsWith("content://")) {
          val ctx = appContext.reactContext ?: throw CodedException("ERR_NO_CONTEXT", "no context", null)
          val input = ctx.contentResolver.openInputStream(android.net.Uri.parse(path)) ?: throw CodedException("ERR_NOT_FOUND", "cannot open $path", null)
          input.use { sha256Hex(it) }
        } else sha256Hex(File(path))
      }
    }
    AsyncFunction("canInstallPackages") {
      ExternalFiles.canInstall(appContext.reactContext ?: throw CodedException("ERR_NO_CONTEXT", "no context", null))
    }
    AsyncFunction("openInstallSettings") {
      ExternalFiles.openInstallSettings(appContext.reactContext ?: throw CodedException("ERR_NO_CONTEXT", "no context", null))
    }
    AsyncFunction("installApk") { path: String ->
      ExternalFiles.installApk(appContext.reactContext ?: throw CodedException("ERR_NO_CONTEXT", "no context", null), File(path))
    }

    AsyncFunction("probeFingerprint") Coroutine { url: String, timeoutMs: Double? ->
      io { PinnedHttp.probeFingerprint(url, (timeoutMs ?: 8000.0).toLong()) }
    }

    AsyncFunction("request") Coroutine { url: String, method: String, headers: Map<String, String>,
                                         bodyText: String?, pin: String?, timeoutMs: Double? ->
      io {
        val r = PinnedHttp.request(
          url, method, headers,
          bodyText?.toByteArray(Charsets.UTF_8),
          pin, (timeoutMs ?: 30000.0).toLong(),
        )
        mapOf(
          "status" to r.status,
          "headers" to r.headers,
          "bodyText" to String(r.body, Charsets.UTF_8),
          "seenFingerprint" to r.seenFingerprint,
        )
      }
    }

    AsyncFunction("downloadToFile") Coroutine { url: String, headers: Map<String, String>, pin: String?,
                                                destPath: String, offset: Double?, timeoutMs: Double? ->
      io {
        PinnedHttp.downloadToFile(
          url, headers, pin, File(destPath), (offset ?: 0.0).toLong(), (timeoutMs ?: 30000.0).toLong(),
        ).toDouble()
      }
    }
  }

  private fun networkTransports(ctx: Context): List<String> {
    val cm = ctx.getSystemService(Context.CONNECTIVITY_SERVICE) as ConnectivityManager
    val caps = cm.activeNetwork?.let { cm.getNetworkCapabilities(it) } ?: return emptyList()
    val names = listOf(
      NetworkCapabilities.TRANSPORT_WIFI to "wifi",
      NetworkCapabilities.TRANSPORT_ETHERNET to "ethernet",
      NetworkCapabilities.TRANSPORT_CELLULAR to "cellular",
      NetworkCapabilities.TRANSPORT_VPN to "vpn",
      NetworkCapabilities.TRANSPORT_BLUETOOTH to "bluetooth",
    )
    return names.filter { caps.hasTransport(it.first) }.map { it.second }
  }
}
