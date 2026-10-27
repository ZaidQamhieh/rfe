# Sidecar protocol (version 1)

The Go agent runs hot components as child processes (`rfe-indexd`, later `rfe-thumbd`). The agent stays the
security boundary: it authenticates devices, decides roots and jails, and re-checks everything a sidecar returns.
A sidecar never decides access. This file is the contract both sides follow; the REST contract stays in
`openapi.yaml`.

## Transport

The agent starts the sidecar and talks to it over the child's **stdin and stdout**. There are no ports or sockets,
so it behaves the same on Linux, macOS and Windows, and the child ends when the agent does (stdin closes).
**stderr is for logs only**; the agent copies it into its own log with a `sidecar <name>:` prefix. stdout carries
frames and nothing else.

## Frames

```
u32 big-endian length | u8 kind | payload        (length counts the kind byte and the payload)
```

| kind | payload |
|---|---|
| 0 | one JSON message |
| 1 | raw bytes belonging to the JSON frame just before it (that message sets `"body": true`) |

A sender writes a JSON frame and its body under one lock, so a receiver always sees them back to back. A frame
longer than 160 MiB, or of length 0, is a protocol error: the receiver closes the connection and the agent
restarts the sidecar.

## Handshake

The sidecar's first frame is a hello. The agent waits up to 5 seconds, requires `kind == "hello"`, `proto == 1`
and the expected `name`, and otherwise kills the process.

```json
{"kind":"hello","proto":1,"name":"rfe-indexd","version":"0.1.0"}
```

## Requests and responses

The agent sends `{"id": <u64>, "op": "<name>", ...fields}`. Ids are unique per connection. Requests run
concurrently and responses may arrive in any order, matched by `id`.

Success: `{"id":N,"ok":true, ...fields}`. Failure: `{"id":N,"ok":false,"code":"...","message":"..."}`.

| code | meaning |
|---|---|
| `BAD_REQUEST` | malformed or unknown request |
| `NOT_SUPPORTED` | the input is not something this sidecar can process (maps to the Go `ErrNotSupported`) |
| `TOO_LARGE` | input over a limit |
| `BUSY` | an exclusive operation is already running |
| `CANCELED` | the request was canceled |
| `INTERNAL` | the sidecar failed (including a panic); the agent falls back to its own code |

**Cancel:** `{"id":M,"op":"cancel","target":N}` asks the sidecar to stop request N early; it answers N with
`CANCELED` (or its normal result if it already finished) and answers the cancel itself with `ok`.

**Ping:** `{"op":"ping"}` answers `{"ok":true}`; the agent uses it for health checks.

## rfe-indexd operations

Paths are absolute, UTF-8, and use the platform separator. Entries whose names are not valid UTF-8 are skipped.

- `index.build` `{roots:[string], maxEntries, maxBytes}` walks the roots in parallel (rooted, no-follow directory
  handles), swaps in the new snapshot, and answers `{entries, truncated, buildMs, bytes}`. One build at a time
  (`BUSY` otherwise). Queries keep using the old snapshot until the swap.
- `index.stats` answers `{ready, entries, truncated, buildMs, bytes, live}` where
  `live` is `{watching, watches, updates, rebuilds, error}`.
- `index.query` `{filters, roots:[string], limit}` answers `{ready, entries:[Entry], truncated}`. `ready:false`
  means no snapshot yet; the agent walks live instead. `filters` is
  `{glob, needle, types, exts, minSize, maxSize, modAfterNs, modBeforeNs}` already validated by the agent
  (lowercase glob in Go `path.Match` syntax, or a lowercase substring). Results come in depth-first, name-sorted
  order, the order Go's `fs.WalkDir` yields. `truncated` is true when `limit` was reached or the index itself was
  cut by its budget.
- `recents.index` `{roots:[string], limit}` answers `{ready, entries, partial}` with the newest files from the live
  index, in memory. `ready` is false (no entries) when it cannot answer exactly: no index yet, the file watcher is
  not healthy, the index was cut by its budget, or a root is not exactly an indexed root (a sub-folder is walked, as
  symlinks resolve against the root). The agent then calls
  `recents.scan`.
- `recents.scan` `{roots:[string], limit, budgetMs}` answers `{entries:[Entry], partial}` with the newest files
  (not directories) first. `partial` is true when `budgetMs` cut the scan short.

**Live updates.** After a build the sidecar watches the file system (per directory with inotify on Linux, the
tree recursively with FSEvents and ReadDirectoryChangesW elsewhere). Events only mark a directory dirty; once they
have been quiet for 250 ms (or 2 s after the first) the dirty directories are re-listed and an updated snapshot is
swapped in, sharing every chunk nothing touched. The directory's own record in its parent is refreshed too. A
batch of more than 4096 directories, a watcher overflow, or an index that was cut by its budget (or would outgrow
it) triggers a full rebuild instead. `live.watching` is true once every directory has a watch; when registration
fails (usually the inotify watch limit) `live.error` says why and the agent keeps its short re-walk interval. A
change made while the watches are being installed is picked up by the next full build, which the agent still runs
periodically (30 minutes with live updates, 5 without).

Walk rules match the Go agent: hidden directories (leading `.`), `node_modules`, and on Linux `/proc /sys /dev
/sysroot` are pruned (never the root itself); hidden files are kept; symlinks are listed but never followed, and a
link whose target is a directory inside the root reports `isDir: true`.

**Entry**:
`{path, size, mtimeNs, ctimeNs, mode, isDir, isSymlink, symlinkTarget?}` where `mode` holds Go `fs.FileMode` bits
and `ctimeNs` follows the Go agent's per-OS "created" rule. The agent derives the MIME type, the mode string and
the timestamps from these fields.

## What the agent must do with every response

1. Re-check each returned `path` is absolute, clean (`filepath.Clean(p) == p`) and under one of the roots it
   asked for; drop anything else.
2. Treat any error, malformed JSON, protocol violation or timeout as "sidecar unavailable" and use its own
   implementation.
3. Never pass user input to a sidecar unvalidated (globs, category names and limits are validated in Go first).

## rfe-thumbd operations

The agent opens the file inside its own jail and sends the bytes; the sidecar never sees a path.

- `thumb.render` `{maxSize}` with a body frame holding the source file (at most 64 MiB) answers
  `{width, height, srcWidth, srcHeight, format}` with a body frame holding the JPEG. The longest side is at most
  `maxSize` and the image is never upscaled (Go `imaging.Fit`), the EXIF orientation is applied first, the filter is
  Lanczos and the JPEG quality is 80 with 4:2:0 chroma. Transparency is flattened onto black, as the Go renderer does.
- Formats: JPEG (decoded at a DCT-reduced size when the target is much smaller), PNG, GIF (first frame), WebP, TIFF (gray, RGB, RGBA, 8 or 16 bit; LZW, Deflate or none; no palette or CMYK) and
  uncompressed BMP (1, 4, 8, 24, 32 bit, bottom-up or top-down; alpha ignored, like Go's x/image/bmp).
  `NOT_SUPPORTED` means the sidecar cannot decode it (any other format or variant, a corrupt file): the agent tries
  its own decoder. `TOO_LARGE` means over 40 megapixels: the agent answers "no thumbnail".
- Four renders run at once; more than 256 MiB of queued sources is answered `BUSY`. A render that runs over 30
  seconds ends the process; the supervisor restarts it.

**Sandbox.** All worker threads exist before the sandbox closes. On Linux the process then sets
`RLIMIT_AS` (6 GiB), no core dumps, no file writes, `no_new_privs`, and a seccomp allowlist (read, write, memory,
futex, time, signals, exit, and `clone` for threads only); every other call fails with `ENOSYS`, so it can open
nothing, connect nowhere and start no process. On Windows it joins a job object (6 GiB, no child processes, kill on
close). Elsewhere it relies on handling only stdin and stdout. `rfe-thumbd --sandbox-selftest` proves the refusals.


## Packaging and verification

A release archive holds `rfe`, `rfe-indexd`, `rfe-thumbd` (`.exe` on Windows) and `rfe-sidecars.txt`:

```
rfe-sidecars 1
version 1.4.0
sha256 <hex> rfe-indexd
sha256 <hex> rfe-thumbd
```

The agent looks in `$RFE_SIDECAR_DIR`, then beside its own executable. When the manifest is present, a sidecar that
is not listed or whose sha256 differs is refused, and the started process must announce the manifest `version` in
its `hello` (sidecars embed `RFE_RELEASE_VERSION` at build time). Without a manifest the sidecar is "unverified" and
only starts when `RFE_SIDECARS` names it. Install and update the whole set together.

## Failure handling and counters

- A call that fails because the sidecar was *down* (`sidecar.ErrUnavailable`) lets the caller use its own
  implementation. A call that fails because the sidecar *died while holding the request* (`sidecar.ErrInterrupted`,
  also an `ErrUnavailable`) is different for thumbnails: the file may have killed the sandboxed decoder, so it is never
  decoded in-process. The first interruption returns a transient error; a second on the same file version (path, size,
  mtime) poisons it, answered as "no thumbnail" without calling the sidecar again.
- The agent counts supervisor starts, start failures, crashes and breaker opens per sidecar, plus named events
  (`thumbd.fallback`, `thumbd.interrupted`, `thumbd.poisoned-file`, `thumbd.unsupported-format`,
  `indexd.search-fallback`, `indexd.search-not-ready`, `indexd.recents-fallback`, `<name>.refused`,
  `<name>.crash`, `<name>.breaker-open`). The daemon writes them to `<data dir>/sidecar-stats.json` every 30 s and
  `rfe status` prints them with their age. `unsupported-format` is expected (e.g. TIFF); the others are not.
