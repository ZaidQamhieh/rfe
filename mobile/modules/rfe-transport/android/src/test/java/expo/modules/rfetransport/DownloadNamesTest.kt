package expo.modules.rfetransport

import org.junit.Assert.assertEquals
import org.junit.Test

class DownloadNamesTest {
  @Test fun theFirstCandidateIsTheNameItself() {
    assertEquals("g1.bin", DownloadNames.candidate("g1.bin", 0))
  }

  @Test fun laterCandidatesNumberTheNameBeforeItsExtension() {
    assertEquals("g1 (1).bin", DownloadNames.candidate("g1.bin", 1))
    assertEquals("g1 (7).bin", DownloadNames.candidate("g1.bin", 7))
  }

  @Test fun onlyTheLastExtensionIsKept() {
    assertEquals("a.tar (2).gz", DownloadNames.candidate("a.tar.gz", 2))
  }

  @Test fun aNameWithoutAnExtensionOrWithOnlyALeadingDotIsNumberedAtTheEnd() {
    assertEquals("README (1)", DownloadNames.candidate("README", 1))
    assertEquals(".bashrc (1)", DownloadNames.candidate(".bashrc", 1))
  }

  @Test fun firstFreeSkipsEveryTakenNameCaseInsensitively() {
    assertEquals("g1.bin", DownloadNames.firstFree("g1.bin", emptySet()))
    assertEquals("g1 (1).bin", DownloadNames.firstFree("g1.bin", setOf("g1.bin")))
    assertEquals("g1 (2).bin", DownloadNames.firstFree("g1.bin", setOf("G1.BIN", "g1 (1).bin")))
  }
}
