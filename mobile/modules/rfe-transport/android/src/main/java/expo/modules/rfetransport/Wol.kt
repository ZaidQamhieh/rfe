package expo.modules.rfetransport

import java.net.DatagramPacket
import java.net.DatagramSocket
import java.net.InetAddress

/** Wake-on-LAN magic packet broadcast to 255.255.255.255:9 (port of core/platform/wol.dart). */
object Wol {
  fun parseMac(mac: String): ByteArray? {
    val parts = mac.split(":")
    if (parts.size != 6) return null
    return try {
      ByteArray(6) { i -> require(parts[i].length in 1..2); parts[i].toInt(16).toByte() }
    } catch (_: Exception) {
      null
    }
  }

  fun packet(mac: ByteArray): ByteArray {
    val out = ByteArray(6 + 16 * 6)
    for (i in 0 until 6) out[i] = 0xFF.toByte()
    for (i in 0 until 16) System.arraycopy(mac, 0, out, 6 + i * 6, 6)
    return out
  }

  fun send(mac: String): Boolean {
    val bytes = parseMac(mac) ?: return false
    return try {
      DatagramSocket().use { s ->
        s.broadcast = true
        val p = packet(bytes)
        s.send(DatagramPacket(p, p.size, InetAddress.getByName("255.255.255.255"), 9))
      }
      true
    } catch (_: Exception) {
      false
    }
  }
}
