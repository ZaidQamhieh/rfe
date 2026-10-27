package expo.modules.rfetransport

import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import okio.Buffer
import org.junit.After
import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertThrows
import org.junit.Before
import org.junit.Test
import java.io.File
import java.io.IOException
import java.nio.file.Files

class PublicHttpTest {
  private val server = MockWebServer()
  private lateinit var dir: File
  private val http = PublicHttp(allowCleartext = true)
  private val data = ByteArray(300_000) { (it * 7).toByte() }

  @Before fun setUp() {
    server.start()
    dir = Files.createTempDirectory("public-http").toFile()
  }

  @After fun tearDown() {
    server.shutdown()
    dir.deleteRecursively()
  }

  private fun body(bytes: ByteArray) = MockResponse().setBody(Buffer().write(bytes))

  @Test fun `downloads a whole file`() {
    server.enqueue(body(data))
    val f = File(dir, "a.apk")
    assertEquals(data.size.toLong(), http.download("t", server.url("/a.apk").toString(), f, 0))
    assertArrayEquals(data, f.readBytes())
  }

  @Test fun `resumes with a Range and appends the rest`() {
    val f = File(dir, "a.apk").also { it.writeBytes(data.copyOfRange(0, 100_000)) }
    server.enqueue(body(data.copyOfRange(100_000, data.size)).setResponseCode(206))
    http.download("t", server.url("/a.apk").toString(), f, 100_000)
    assertEquals("bytes=100000-", server.takeRequest().getHeader("Range"))
    assertArrayEquals(data, f.readBytes())
  }

  @Test fun `a server that ignores the Range invalidates the partial file`() {
    val f = File(dir, "a.apk").also { it.writeBytes(data.copyOfRange(0, 100_000)) }
    server.enqueue(body(data))
    assertThrows(RangeNotHonoured::class.java) { http.download("t", server.url("/a.apk").toString(), f, 100_000) }
    assertFalse(f.exists())
  }

  @Test fun `refuses cleartext by default and reports an HTTP error`() {
    assertThrows(IOException::class.java) { PublicHttp().download("t", server.url("/a.apk").toString(), File(dir, "b"), 0) }
    server.enqueue(MockResponse().setResponseCode(404))
    assertThrows(IOException::class.java) { http.download("t", server.url("/x").toString(), File(dir, "c"), 0) }
  }

  @Test fun `a transient server error on resume keeps the partial file`() {
    val f = File(dir, "a.apk").also { it.writeBytes(data.copyOfRange(0, 100_000)) }
    server.enqueue(MockResponse().setResponseCode(503))
    val e = assertThrows(IOException::class.java) { http.download("t", server.url("/a.apk").toString(), f, 100_000) }
    assertFalse(e is RangeNotHonoured)
    assertEquals(100_000L, f.length())
  }

  @Test fun `a 416 on resume drops the stale partial file`() {
    val f = File(dir, "a.apk").also { it.writeBytes(data.copyOfRange(0, 100_000)) }
    server.enqueue(MockResponse().setResponseCode(416))
    assertThrows(RangeNotHonoured::class.java) { http.download("t", server.url("/a.apk").toString(), f, 100_000) }
    assertFalse(f.exists())
  }
}
