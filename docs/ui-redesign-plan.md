# UI Redesign Plan — RFE

**Status snapshot: 2026-09-28.** This execution brief was written on 2026-06-12
and its main UI waves have since been implemented. Waves A–E are delivered.
Wave F's theme and accessibility foundations are present; the tablet two-pane
stretch goal and full-device text-scaling/landscape visual acceptance remain
open. Visual direction lives in `docs/ui-design-spec.md` (Expressive M3,
approved 2026-06-12).

This file preserves the original acceptance brief and sequencing notes. The
test count and release instructions below were written before the waves landed;
use `docs/development.md` and the current GitHub workflows for today's commands
and release process.

---

## Constraints (do not violate)

- **flutter_riverpod pinned to 2.6.1** (Notifier API). No Riverpod 3.
- **Flutter 3.29.0, Impeller DISABLED** (Skia fallback in AndroidManifest due to
  glyph-atlas corruption on the user's Samsung). Avoid expensive per-frame
  blurs/shaders; prefer cheap M3 surfaces and opacity/transform animations.
- Android-first (OTA APK updater). Don't break `update_tile.dart`'s flow.
- Preserve behavior: TOFU cert pinning, pairing flow, pop-with-parent-path
  search navigation, transfer engine semantics (recently rebuilt — touch its UI,
  not its logic).
- Keep CI green; every wave ends with analyze + full test suite passing.

## Current UI inventory (2026-06-12)

| Area | Files | Lines | State |
|---|---|---|---|
| Explorer | `explorer_screen.dart` | 1,359 | God-file: ~15 widget classes in one file |
| Hosts | `host_list_screen.dart` | 518 | Functional cards, ping status |
| Transfers | `transfer_manager.dart` | 301 | Plain list, no speed/ETA |
| Settings | `settings_screen.dart` + `update_tile.dart` | 436+ | Functional |
| Search | `search_screen.dart` | — | Just redesigned (v1.7.0) — leave alone |
| Preview | image/pdf/text/video + `preview_common.dart` | — | Per-type viewers, no swipe-between |
| Theme | `tokens.dart` (56), `app_theme.dart` (123), `motion.dart` | — | Thin M3 token layer |

Known UI debt: `_formatSize` duplicated ~5× (explorer_screen, meta_sheet,
search area, transfer_manager, preview_common); Move/Copy destination picker is
a raw type-the-path TextField (`explorer_screen.dart` destination dialog);
`AgentClient.drives()` + `Drive` model exist but have **no UI** (Windows hosts
unusable beyond the faked `/` root); favorites exist but are buried in a sheet.

---

## Wave A — Foundations (precondition for everything else)

**Goal:** make the codebase safe to restyle. No visual changes yet.

1. Split `explorer_screen.dart` into `features/explorer/widgets/`:
   `breadcrumb_bar.dart`, `entry_tile.dart`, `entry_grid_cell.dart`,
   `selection_bar.dart`, `create_menu.dart`, `destination_dialog.dart`,
   `favorites_sheet.dart`. Mechanical move, zero behavior change.
2. Create `core/ui/format.dart`: single `formatSize`, `formatDate` — delete all
   duplicates and re-point call sites.
3. Expand `core/theme/tokens.dart`: spacing scale (4/8/12/16/24/32), radius
   scale, elevation/surface roles, duration tokens (sync with `motion.dart`).
4. Unified `EntryLeading` widget (icon-or-thumbnail by type, one category→icon
   map — server already has the category table; mirror its categories).

**Acceptance:** zero visual diff intended; analyze clean; all tests pass; add
widget tests for `EntryTile` (file vs folder vs selected states).
**Agents:** 1 Sonnet (it's one coherent refactor; parallelism would conflict).

## Wave B — Host dashboard (first screen impression)

**Goal:** turn the host list into a dashboard.

1. Redesigned host cards: online/offline status dot with last-seen, agent
   version chip, LAN vs Tailscale indicator (which address is active).
2. **Storage gauges:** call `AgentClient.drives()` (currently dead code) and
   render per-drive used/total bars on the card (Linux: roots; Windows: drives).
3. Quick actions row per host: Browse, Search, Transfers, Settings.
4. Update-available banner styled as M3 banner instead of dialog-only.
5. Empty state: polished "pair your first PC" hero with the QR scan CTA.

**Acceptance:** drives endpoint wired with graceful fallback when the agent
predates it; offline hosts render cached info dimmed, not error-red.
**Agents:** 1 Sonnet (host_list_screen + a new `widgets/host_card.dart`).

## Wave C — Explorer redesign (the core screen)

**Goal:** modern file-manager ergonomics.

1. Breadcrumb bar → horizontally scrollable M3 segmented chips with overflow
   menu for deep paths; long-press a crumb = copy path.
2. **View options**: list/grid toggle persisted per host (SharedPreferences),
   grid density (comfortable/compact), sort control moved into one popover
   (name/size/date + direction) — persisted.
3. Selection mode: top app bar morphs into contextual action bar (count,
   select-all, invert), bottom action bar for move/copy/delete/share.
4. **Folder-browser destination picker**: replace the type-a-path dialog with a
   navigable mini-explorer bottom sheet (breadcrumbs + folder list + "new
   folder" + confirm). This is the single biggest UX hole.
5. **Windows drive picker**: when `host.os == windows`, root view lists drives
   (from Wave B's wiring) instead of `/`.
6. Favorites: pin row at top of root listing + star action in tiles.
7. Pull-to-refresh everywhere it's missing; skeleton loaders already exist.

**Acceptance:** all explorer state changes go through `ExplorerNotifier` (no
direct client calls from widgets); destination picker reuses the jailed `list`
API; persisted prefs survive restart (test with mock SharedPreferences).
**Agents:** 2 Sonnet in sequence (C1: items 1–3+7 visual layer; C2: items 4–6
which need state/provider work). Parallel would collide on explorer files.

## Wave D — Transfers center

**Goal:** make transfers feel alive.

1. Per-task speed (rolling average) + ETA, computed in the notifier from
   `transferredBytes` deltas — UI only, engine untouched.
2. Group tasks: Active / Queued / Done / Failed sections, collapsible.
3. Swipe actions: pause/resume (left), remove (right) with undo snackbar.
4. Mini progress indicator: a thin LinearProgressIndicator pinned above the
   explorer bottom bar while anything is active, tap → transfer manager.
5. Clear-completed action; failure rows show the error inline with retry.

**Acceptance:** speed/ETA unit-tested (pure function over byte/time samples);
no changes to `transfer_state.dart` engine methods beyond exposing timestamps.
**Agents:** 1 Sonnet.

## Wave E — Preview & polish

1. Swipe between sibling files in preview (PageView over the current listing,
   filtered to previewable types), preloading neighbors' thumbnails.
2. Hero animation tile→preview for images (cheap on Skia; test on device).
3. Unified preview top bar: name, size, share/save/delete, "show in folder".
4. Text preview: syntax highlighting is OUT of scope (package weight); just
   monospace + line numbers toggle.

**Agents:** 1 Sonnet.

## Wave F — Theme, accessibility, tablet

1. Dynamic color (Material You) via `dynamic_color` package with the current
   palette as fallback; light theme audit (app is dark-first today);
   theme mode setting (system/dark/light) in settings.
2. Accessibility: semantics labels on icon buttons, 48dp touch targets,
   `MediaQuery.textScaler` audit (the Impeller bug report mentioned font
   scaling — verify layouts at 1.3× and 2.0× scale).
3. Tablet/landscape: two-pane explorer (tree/list + preview) behind a width
   breakpoint — stretch goal, cut first if the wave runs long.

**Agents:** 1–2 Sonnet (theme vs a11y are disjoint enough to parallelize).

---

## Sequencing & release strategy

- Order: **A → B → C1 → C2 → D → E → F.** A is a hard precondition; B–F are
  independently shippable.
- Ship checkpoints: after C2 (release as **1.8.0**, "explorer update") and after
  F (release as **1.9.0**, "design update"). Don't sit on unreleased waves.
- Each wave: agent works → orchestrator verifies analyze+tests → commit
  (`feat(ui): wave X — ...`) → push (CI must pass) → next wave.
- Estimated total: ~6–8 agent dispatches across 2–3 sessions.

## Out of scope (explicitly)

- WebSocket/SSE live refresh (`/v1/events`) — separate backend feature wave.
- Background transfers (foreground service) — separate engineering wave.
- iOS anything. Content search. Syntax highlighting in text preview.

## Still-open non-UI backlog (don't lose track)

See memory `project-rfe-code-review-2026-06`: cert pin read from plaintext
prefs, address-fallback never returns to LAN, update-dialog Cancel doesn't
cancel the download, 60-min pairing TTL, no git tags. Fix opportunistically
when a wave touches the same files (the update Cancel fix belongs to Wave B's
banner work).
