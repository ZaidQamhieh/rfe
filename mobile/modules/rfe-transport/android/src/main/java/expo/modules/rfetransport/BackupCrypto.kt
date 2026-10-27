package expo.modules.rfetransport

import org.json.JSONException
import org.json.JSONObject
import java.security.SecureRandom
import java.util.Base64
import javax.crypto.AEADBadTagException
import javax.crypto.Cipher
import javax.crypto.Mac
import javax.crypto.spec.GCMParameterSpec
import javax.crypto.spec.SecretKeySpec

class BackupCryptoException(message: String) : Exception(message)

/**
 * The encrypted settings backup, byte-compatible with the Flutter app's `config_backup.dart`: a JSON envelope with
 * PBKDF2-HMAC-SHA256 (200 000 iterations, 16-byte salt) deriving an AES-256-GCM key; the 16-byte tag travels
 * separately as `mac`. The messages are the Flutter ones, so both apps tell the user the same thing.
 */
object BackupCrypto {
  const val ITERATIONS = 200_000
  const val MIN_PASSPHRASE = 12
  private const val MAX_ITERATIONS = 2_000_000
  private const val MAX_SALT = 128
  private const val MAX_NONCE = 64
  private const val MAX_MAC = 64
  private const val MAX_CIPHERTEXT = 32 * 1024 * 1024
  private const val FORMAT = "rfe-backup"
  private const val KDF = "pbkdf2-hmac-sha256"
  private const val SALT_BYTES = 16
  private const val NONCE_BYTES = 12
  private const val TAG_BITS = 128

  private val random = SecureRandom()

  /** PBKDF2 over the UTF-8 passphrase, written out so the byte encoding never depends on the platform provider. */
  internal fun deriveKey(passphrase: String, salt: ByteArray, iterations: Int): ByteArray {
    val mac = Mac.getInstance("HmacSHA256")
    mac.init(SecretKeySpec(passphrase.toByteArray(Charsets.UTF_8), "HmacSHA256"))
    mac.update(salt)
    mac.update(byteArrayOf(0, 0, 0, 1))
    var u = mac.doFinal()
    val t = u.copyOf()
    for (i in 1 until iterations) {
      u = mac.doFinal(u)
      for (j in t.indices) t[j] = (t[j].toInt() xor u[j].toInt()).toByte()
    }
    return t
  }

  fun encrypt(payloadJson: String, passphrase: String): String {
    if (passphrase.length < MIN_PASSPHRASE) throw BackupCryptoException("Passphrase must be at least $MIN_PASSPHRASE characters.")
    val salt = ByteArray(SALT_BYTES).also(random::nextBytes)
    val nonce = ByteArray(NONCE_BYTES).also(random::nextBytes)
    val cipher = Cipher.getInstance("AES/GCM/NoPadding")
    cipher.init(Cipher.ENCRYPT_MODE, SecretKeySpec(deriveKey(passphrase, salt, ITERATIONS), "AES"), GCMParameterSpec(TAG_BITS, nonce))
    val sealed = cipher.doFinal(payloadJson.toByteArray(Charsets.UTF_8))
    val tagAt = sealed.size - TAG_BITS / 8
    val b64 = Base64.getEncoder()
    return JSONObject()
      .put("format", FORMAT).put("v", 1).put("kdf", KDF).put("iter", ITERATIONS)
      .put("salt", b64.encodeToString(salt)).put("nonce", b64.encodeToString(nonce))
      .put("ct", b64.encodeToString(sealed.copyOfRange(0, tagAt))).put("mac", b64.encodeToString(sealed.copyOfRange(tagAt, sealed.size)))
      .toString()
  }

  fun decrypt(envelopeJson: String, passphrase: String): String {
    val env = try { JSONObject(envelopeJson) } catch (_: JSONException) { throw BackupCryptoException("This file is not a valid backup.") }
    if (env.optString("format") != FORMAT) throw BackupCryptoException("This file is not an RFE backup.")
    if (env.opt("v") != 1) throw BackupCryptoException("Unsupported backup version: ${env.opt("v")}.")
    if (env.optString("kdf") != KDF) throw BackupCryptoException("Unsupported key derivation: ${env.optString("kdf")}.")
    val corrupted = BackupCryptoException("This backup file is corrupted.")
    val b64 = Base64.getDecoder()
    val salt: ByteArray
    val nonce: ByteArray
    val ct: ByteArray
    val mac: ByteArray
    val iterations: Int
    try {
      salt = b64.decode(env.getString("salt"))
      nonce = b64.decode(env.getString("nonce"))
      ct = b64.decode(env.getString("ct"))
      mac = b64.decode(env.getString("mac"))
      iterations = env.getInt("iter")
    } catch (_: Exception) {
      throw corrupted
    }
    // Bound every field before the expensive work, so a crafted envelope cannot freeze the app.
    if (iterations < 1 || iterations > MAX_ITERATIONS) throw corrupted
    if (salt.size > MAX_SALT || nonce.isEmpty() || nonce.size > MAX_NONCE || mac.size != TAG_BITS / 8 || ct.size > MAX_CIPHERTEXT) throw corrupted
    val wrong = BackupCryptoException("Incorrect passphrase, or this backup file has been corrupted.")
    if (passphrase.isEmpty()) throw wrong
    val plain = try {
      val cipher = Cipher.getInstance("AES/GCM/NoPadding")
      cipher.init(Cipher.DECRYPT_MODE, SecretKeySpec(deriveKey(passphrase, salt, iterations), "AES"), GCMParameterSpec(TAG_BITS, nonce))
      cipher.doFinal(ct + mac)
    } catch (_: AEADBadTagException) {
      throw wrong
    } catch (_: Exception) {
      throw wrong
    }
    return String(plain, Charsets.UTF_8)
  }
}
