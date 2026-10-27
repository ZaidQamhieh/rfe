package expo.modules.rfetransport

import com.google.crypto.tink.subtle.AesGcmHkdfStreaming
import java.io.File
import java.io.FileInputStream
import java.io.FileOutputStream
import java.io.IOException
import java.security.MessageDigest
import java.security.SecureRandom

/** A stored body failed authentication (tampered, truncated, or bound to another host/path); the entry has been deleted. */
class OfflineBodyIntegrityException : IOException("offline cache entry failed authentication")

/**
 * Encrypted-at-rest copies of file bodies from pinned folders, readable when the host is unreachable.
 *
 * Each body is one Tink AES-256-GCM-HKDF streaming ciphertext (64 KiB segments, so truncation and reordering
 * are detected) whose associated data binds it to its host and path: a file moved to another name, or copied
 * over another entry, cannot authenticate. The 32-byte input key lives in platform secure storage, never beside
 * the files. File names are hashes, so the directory reveals neither host ids nor paths; the host prefix lets a
 * host's entries be dropped on unpair. Writes go through a temp file, so readers see the old or the new body.
 */
class OfflineBodyStore(private val dir: File, private val keyProvider: () -> ByteArray) {
  private val random = SecureRandom()

  init { dir.mkdirs() }

  private fun hex(bytes: ByteArray) = bytes.joinToString("") { "%02x".format(it) }
  private fun sha256(s: String) = hex(MessageDigest.getInstance("SHA-256").digest(s.toByteArray(Charsets.UTF_8)))
  private fun hostPrefix(hostId: String) = sha256("host:$hostId").take(16) + "-"
  private fun fileFor(hostId: String, path: String) = File(dir, hostPrefix(hostId) + sha256("$hostId\u0000$path"))
  private fun aad(hostId: String, path: String) = "rfe:offline-body-cache:v3\u0000$hostId\u0000$path".toByteArray(Charsets.UTF_8)
  private fun aead(): AesGcmHkdfStreaming = AesGcmHkdfStreaming(keyProvider(), "HmacSha256", 32, SEGMENT_BYTES, 0)

  /** Encrypts [src] into the store under (hostId, path), replacing any previous body. */
  fun put(hostId: String, path: String, src: File) {
    val target = fileFor(hostId, path)
    val tmp = File(dir, target.name + ".tmp-" + random.nextLong().toString(16))
    try {
      FileInputStream(src).use { input ->
        aead().newEncryptingStream(FileOutputStream(tmp), aad(hostId, path)).use { out -> input.copyTo(out, 64 * 1024) }
      }
      // rename(2) replaces the target atomically; a delete first would leave a moment with neither body (audit F-115).
      if (!tmp.renameTo(target)) throw IOException("could not publish cache entry")
    } finally {
      tmp.delete()
    }
  }

  /** Decrypts the entry to [dest]; false when there is none. A body that fails authentication is deleted and reported. */
  fun restore(hostId: String, path: String, dest: File): Boolean {
    val src = fileFor(hostId, path)
    if (!src.exists()) return false
    val tmp = File(dest.path + ".dec-" + random.nextLong().toString(16))
    var unreadable = false
    try {
      dest.parentFile?.mkdirs()
      FileInputStream(src).use { raw ->
        // A header Tink rejects while the stream is built is as unreadable as a bad body.
        val stream = try { aead().newDecryptingStream(raw, aad(hostId, path)) } catch (e: IOException) { unreadable = true; throw e }
        stream.use { plain ->
          FileOutputStream(tmp).use { out ->
            val buf = ByteArray(64 * 1024)
            while (true) {
              // Only a failure to READ the encrypted body means it is bad; a full disk or an unwritable
              // destination must not destroy a good cached copy (audit F-108).
              val n = try { plain.read(buf) } catch (e: IOException) { unreadable = true; throw e }
              if (n < 0) break
              out.write(buf, 0, n)
            }
          }
        }
      }
      if (!tmp.renameTo(dest)) throw IOException("could not publish restored file")
      return true
    } catch (e: IOException) {
      // Tink reports authentication failures as IOException("Tag mismatch") and short reads as truncation.
      if (!unreadable) throw e
      src.delete()
      throw OfflineBodyIntegrityException()
    } finally {
      tmp.delete()
    }
  }

  fun has(hostId: String, path: String): Boolean = fileFor(hostId, path).exists()

  /** Bytes used by every entry, counting in-progress writes so concurrent callers keep the same conservative budget. */
  fun totalBytes(): Long = dir.listFiles()?.filter { it.isFile }?.sumOf { it.length() } ?: 0L

  fun remove(hostId: String, path: String) { fileFor(hostId, path).delete() }

  fun evictHost(hostId: String) {
    val prefix = hostPrefix(hostId)
    dir.listFiles()?.filter { it.name.startsWith(prefix) }?.forEach { it.delete() }
  }

  companion object {
    const val SEGMENT_BYTES = 64 * 1024
    const val KEY_BYTES = 32

    fun newKey(): ByteArray = ByteArray(KEY_BYTES).also { SecureRandom().nextBytes(it) }
  }
}
