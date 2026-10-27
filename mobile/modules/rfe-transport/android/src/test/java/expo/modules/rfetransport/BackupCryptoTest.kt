package expo.modules.rfetransport

import org.json.JSONObject
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotEquals
import org.junit.Assert.assertThrows
import org.junit.Assert.assertTrue
import org.junit.Test
import java.util.Base64

class BackupCryptoTest {
  private val flutterEnvelope = javaClass.getResource("/flutter-backup.json")!!.readText()

  @Test fun `opens a backup written by the Flutter app, including a non-ascii passphrase`() {
    val payload = JSONObject(BackupCrypto.decrypt(flutterEnvelope, "correct horse ünï battery"))
    assertEquals("dark", payload.getJSONObject("prefs").getJSONObject("app.theme").getString("v"))
    assertEquals("tök€n-1", payload.getJSONObject("secure").getString("rfe_token_h1"))
    assertEquals(1, payload.getInt("version"))
  }

  @Test fun `a wrong passphrase and a tampered file are both rejected with the Flutter message`() {
    val wrong = assertThrows(BackupCryptoException::class.java) { BackupCrypto.decrypt(flutterEnvelope, "wrong passphrase!!") }
    assertEquals("Incorrect passphrase, or this backup file has been corrupted.", wrong.message)
    val env = JSONObject(flutterEnvelope)
    val ct = Base64.getDecoder().decode(env.getString("ct")).also { it[0] = (it[0].toInt() xor 1).toByte() }
    env.put("ct", Base64.getEncoder().encodeToString(ct))
    assertThrows(BackupCryptoException::class.java) { BackupCrypto.decrypt(env.toString(), "correct horse ünï battery") }
  }

  @Test fun `encrypt then decrypt round-trips and uses a fresh salt and nonce each time`() {
    val a = BackupCrypto.encrypt("""{"k":"v"}""", "twelve chars!!")
    val b = BackupCrypto.encrypt("""{"k":"v"}""", "twelve chars!!")
    assertEquals("""{"k":"v"}""", BackupCrypto.decrypt(a, "twelve chars!!"))
    assertNotEquals(JSONObject(a).getString("salt"), JSONObject(b).getString("salt"))
    assertNotEquals(JSONObject(a).getString("ct"), JSONObject(b).getString("ct"))
    val env = JSONObject(a)
    assertEquals(200000, env.getInt("iter"))
    assertEquals(16, Base64.getDecoder().decode(env.getString("mac")).size)
  }

  @Test fun `a short passphrase is refused before any work`() {
    val e = assertThrows(BackupCryptoException::class.java) { BackupCrypto.encrypt("{}", "short") }
    assertEquals("Passphrase must be at least 12 characters.", e.message)
  }

  @Test fun `bad envelopes get the specific message`() {
    fun msg(s: String) = assertThrows(BackupCryptoException::class.java) { BackupCrypto.decrypt(s, "twelve chars!!") }.message
    assertEquals("This file is not a valid backup.", msg("nope"))
    assertEquals("This file is not an RFE backup.", msg("""{"format":"x"}"""))
    assertEquals("Unsupported backup version: 2.", msg("""{"format":"rfe-backup","v":2}"""))
    val inflated = JSONObject(flutterEnvelope).put("iter", 999_999_999)
    assertEquals("This backup file is corrupted.", msg(inflated.toString()))
    assertTrue(msg("""{"format":"rfe-backup","v":1,"kdf":"pbkdf2-hmac-sha256"}""")!!.contains("corrupted"))
  }
}
