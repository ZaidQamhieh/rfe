package expo.modules.rfetransport

/** Picks the file name a download is saved under in the shared Downloads folder when a file of that name may already be there. */
object DownloadNames {
  /**
   * The [n]th candidate for [name]: [name] itself for 0, otherwise "base (n).ext" (the number goes before the last
   * extension, as Android numbers a duplicate). A name with no extension, or only a leading dot, is numbered at the end.
   */
  fun candidate(name: String, n: Int): String {
    if (n <= 0) return name
    val dot = name.lastIndexOf('.')
    return if (dot <= 0) "$name ($n)" else "${name.substring(0, dot)} ($n)${name.substring(dot)}"
  }

  /** The first candidate for [name] that is not in [taken] (compared ignoring case, as shared storage does). */
  fun firstFree(name: String, taken: Set<String>): String {
    val lower = taken.mapTo(HashSet()) { it.lowercase() }
    var n = 0
    while (candidate(name, n).lowercase() in lower) n++
    return candidate(name, n)
  }
}
