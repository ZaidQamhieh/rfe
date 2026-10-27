package expo.modules.rfetransport

import org.junit.After
import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertThrows
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import java.io.File
import java.nio.file.Files

class OfflineBodyStoreTest {
  private lateinit var root: File
  private lateinit var store: OfflineBodyStore
  private val key = OfflineBodyStore.newKey()

  @Before fun setUp() {
    root = Files.createTempDirectory("rfe-obc").toFile()
    store = OfflineBodyStore(File(root, "cache"), { key })
  }

  @After fun tearDown() { root.deleteRecursively() }

  private fun file(name: String, bytes: ByteArray) = File(root, name).apply { writeBytes(bytes) }
  private fun sample(n: Int) = ByteArray(n) { (it * 31 + 7).toByte() }

  @Test fun roundTripsSmallEmptyAndMultiSegmentBodies() {
    for (n in listOf(0, 1, 100, 65_536, 65_537, 300_000)) {
      val src = file("s$n", sample(n))
      store.put("h", "/p/$n", src)
      val out = File(root, "out$n")
      assertTrue(store.restore("h", "/p/$n", out))
      assertArrayEquals("size $n", src.readBytes(), out.readBytes())
    }
  }

  @Test fun storedBytesAreNotPlaintextAndNamesRevealNothing() {
    val secret = "top-secret-marker-".repeat(50).toByteArray()
    store.put("host-one", "/private/report.pdf", file("s", secret))
    val stored = File(root, "cache").listFiles()!!.single()
    assertFalse(String(stored.readBytes(), Charsets.ISO_8859_1).contains("top-secret-marker"))
    assertFalse(stored.name.contains("report") || stored.name.contains("host-one"))
  }

  @Test fun tamperingIsDetectedAndTheEntryDeleted() {
    store.put("h", "/a", file("s", sample(200_000)))
    val stored = File(root, "cache").listFiles()!!.single()
    val bytes = stored.readBytes()
    bytes[bytes.size / 2] = (bytes[bytes.size / 2].toInt() xor 1).toByte()
    stored.writeBytes(bytes)
    assertThrows(OfflineBodyIntegrityException::class.java) { store.restore("h", "/a", File(root, "o")) }
    assertFalse(store.has("h", "/a"))
    assertFalse(File(root, "o").exists())
  }

  @Test fun truncationIsDetected() {
    store.put("h", "/a", file("s", sample(300_000)))
    val stored = File(root, "cache").listFiles()!!.single()
    val bytes = stored.readBytes()
    stored.writeBytes(bytes.copyOf(bytes.size - 70_000))
    assertThrows(OfflineBodyIntegrityException::class.java) { store.restore("h", "/a", File(root, "o")) }
  }

  @Test fun anUnwritableDestinationKeepsTheCachedCopy() {
    store.put("h", "/a", file("s", sample(1000)))
    val blocker = file("blocker", sample(1)) // a file where the destination's folder should be
    assertThrows(java.io.IOException::class.java) { store.restore("h", "/a", File(blocker, "out")) }
    assertTrue("a good copy must survive a failure that was not its fault", store.has("h", "/a"))
    val out = File(root, "ok")
    assertTrue(store.restore("h", "/a", out))
    assertArrayEquals(sample(1000), out.readBytes())
  }

  @Test fun anEntryCannotBeReadUnderAnotherHostOrPath() {
    store.put("h1", "/a", file("s", sample(1000)))
    val src = File(root, "cache").listFiles()!!.single()
    // Copy the ciphertext onto the slot another (host, path) would use: the bound associated data must reject it.
    store.put("h2", "/b", file("other", sample(10)))
    val slot = File(root, "cache").listFiles()!!.first { it != src }
    slot.writeBytes(src.readBytes())
    assertThrows(OfflineBodyIntegrityException::class.java) { store.restore("h2", "/b", File(root, "o")) }
  }

  @Test fun aWrongKeyCannotDecrypt() {
    store.put("h", "/a", file("s", sample(1000)))
    val other = OfflineBodyStore(File(root, "cache"), { OfflineBodyStore.newKey() })
    assertThrows(OfflineBodyIntegrityException::class.java) { other.restore("h", "/a", File(root, "o")) }
  }

  @Test fun missingEntriesAreNotErrors() {
    assertFalse(store.has("h", "/nope"))
    assertFalse(store.restore("h", "/nope", File(root, "o")))
  }

  @Test fun replacingRemovingAndEvictingHosts() {
    store.put("h1", "/a", file("s1", sample(10)))
    store.put("h1", "/a", file("s2", sample(20)))
    store.put("h1", "/b", file("s3", sample(30)))
    store.put("h2", "/a", file("s4", sample(40)))
    val out = File(root, "o")
    store.restore("h1", "/a", out)
    assertEquals(20, out.length())
    assertTrue(store.totalBytes() > 0)
    store.remove("h1", "/b")
    assertFalse(store.has("h1", "/b"))
    store.evictHost("h1")
    assertFalse(store.has("h1", "/a"))
    assertTrue(store.has("h2", "/a"))
    assertEquals(1, File(root, "cache").listFiles()!!.size)
  }

  @Test fun noTempFilesAreLeftBehind() {
    store.put("h", "/a", file("s", sample(100_000)))
    assertTrue(File(root, "cache").listFiles()!!.none { it.name.contains(".tmp-") })
  }
}
