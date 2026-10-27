# Audio Preview (backlog #6, audio half)

## Context

Backlog item **#6 — streaming video/audio previews** was only half done: the
video viewer (`video_preview.dart`, `video_player` + `chewie`) shipped earlier,
but there was no audio viewer. Tapping an audio file fell through to
"No preview available for this file type". This wave adds the audio half.

The agent already serves file bytes with HTTP Range (`GET /v1/content`,
`http.ServeContent`). No agent or `protocol/openapi.yaml` change is needed.

## Why not true streaming / `just_audio`

Same constraint that shaped the video viewer: the agent uses a self-signed cert
(TOFU-pinned) and bearer-token auth. A media player's `networkUrl` path can do
neither, so we can't stream straight from the agent. We reuse the proven
**download-to-temp-then-play** path instead.

We deliberately reuse `video_player` (already a dependency, already proven on
the owner's Skia/Impeller-disabled device) rather than add `just_audio`:
`VideoPlayerController` decodes audio-only files fine, and avoiding a second
native media stack keeps build risk and the off-device verification gap small.

## Design

- `lib/core/ui/format.dart` — new shared `formatDuration(Duration?)` → `m:ss`
  / `h:mm:ss`, clamped at zero (one-formatter convention; no local duplicate).
- `lib/features/preview/preview_common.dart` — `kMaxAudioPreviewBytes` = 100 MB
  cap (audio is smaller than the 300 MB video cap); over it → "too large".
- `lib/features/preview/audio_preview.dart` — `AudioPreviewScreen`, mirrors
  `VideoPreviewScreen`: download to `preview_cache` temp file with progress →
  `VideoPlayerController.file` → custom compact transport (`_AudioTransport`):
  music-note glyph, file name, scrub `Slider`, elapsed/total `formatDuration`
  labels, play/pause (replay at end). Supports `chromeless` for the pager.
  Renders on a normal surface (not the black media canvas), like text/pdf.
- `lib/features/preview/preview.dart` — `_PreviewKind.audio`, `audio/*` MIME +
  `_audioExtensions` fallback (mp3/m4a/aac/wav/flac/ogg/oga/opus/wma/aiff/aif),
  and routing in `_viewerFor`. Audio thus joins `isPreviewable` / the swipeable
  `PreviewPager` automatically; only images preload (audio is too heavy).

## Tests (pure / headless only — matches the video viewer, which has none)

- `format_test.dart` — `formatDuration` group (zero/null, padding, hours,
  negative clamp).
- `preview_siblings_test.dart` — audio extensions + `audio/*` MIME are
  previewable and counted among pager siblings.

Playback itself (native player init) can only be verified on a real phone —
the known #6 off-device gap, same as video.
