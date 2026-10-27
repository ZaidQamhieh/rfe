# UI Design Spec — Expressive M3

**Status: APPROVED DIRECTION, NOT IMPLEMENTED.** Written 2026-06-12. Companion
to `docs/ui-redesign-plan.md` (waves A–F): the plan says *what and when*, this
spec says *what it looks like*. The owner chose **Expressive M3** over a dense
pro-tool look and a refine-only pass.

Direction in one line: bold Material You — tonal surface cards, large corner
radii, color-forward, generous touch targets — like Google's M3 showcase apps,
while staying honest about the constraints below.

## Constraints that shape visual decisions

- **Skia renderer** (Impeller disabled): no backdrop blurs, no frosted glass,
  no large animated shadows. Depth comes from **tonal surface color**, not
  elevation shadows. Animate transforms/opacity only.
- Dark-first (current user habit), but every spec below must read correctly in
  light mode too (Wave F ships the toggle).
- The chosen direction's known risk is **density** — mitigated by a mandatory
  comfortable/compact density setting (see Explorer).

---

## 1. Foundations

### Color
- **Dynamic color** (Material You) from the user's wallpaper via
  `dynamic_color`, with seed fallback `#4F5B92` (the current indigo family —
  sample the exact current primary from `tokens.dart` at implementation time
  and use it as the seed so the fallback feels like home).
- Dark theme surfaces use M3 tonal elevation: `surface` → `surfaceContainer`
  → `surfaceContainerHigh` for card/sheet stacking. **Never** pure black cards
  on pure black background.
- Color roles, not hex, everywhere: `primaryContainer` for selected states and
  highlights, `secondaryContainer` for chips/badges, `tertiaryContainer` for
  storage gauges and progress accents, `errorContainer` for failed states.
- One accent per surface: a card may use at most one container-color block
  (e.g. the gauge) — everything else neutral.

### Typography (M3 roles)
| Use | Role | Notes |
|---|---|---|
| Screen titles | `headlineSmall` | Host name on dashboard gets `headlineMedium` |
| Card titles / entry names | `titleMedium` | Single line, ellipsize middle for filenames (keep extension visible) |
| Metadata (size · date) | `bodySmall` on `onSurfaceVariant` | The `·` separator everywhere, never `|` or `-` |
| Section labels | `labelLarge` | Sentence case, not ALL CAPS |
| Buttons/chips | `labelLarge` | |

### Shape
| Component | Radius |
|---|---|
| Dashboard host cards | 24 |
| Bottom sheets | 28 top corners |
| Entry tiles (list) | 16 (when selected/hovered surface shows) |
| Grid cells, thumbnails | 16 |
| Chips | full (stadium) |
| Buttons: primary actions filled, secondary tonal | full |
| Dialogs | 28 |

### Spacing
Token scale (Wave A adds to `tokens.dart`): 4 / 8 / 12 / 16 / 24 / 32.
- Screen edge padding: 16. Card internal padding: 16 (20 for the dashboard
  hero card). Gap between cards: 12. List tile vertical padding: comfortable
  12, compact 6.

### Motion (Skia-safe)
- Durations: 150ms (state changes), 250ms (component transitions), 350ms
  (screen transitions). Curve: `Curves.easeOutCubic` standard,
  `easeInOutCubicEmphasized` for screen-level.
- Screen transitions: M3 fadeThrough (forward) / shared-axis horizontal
  (explorer drill-down). Hero only for image tile → image preview.
- Every async action gets immediate feedback: ripple + (existing) haptics from
  `core/ui/feedback.dart`.

### Iconography
- `Icons.*_rounded` variants everywhere, filled style for the selected/active
  state, outlined for inactive. One category→icon map in the unified
  `EntryLeading` (Wave A), colored per category with container colors at 12%
  opacity backgrounds:
  folder=primary, image=tertiary, video=pink-ish secondary, audio=green
  harmonized, document=blue harmonized, archive=amber harmonized, other=neutral.

---

## 2. Host dashboard (Wave B)

```
╭──────────────────────────────────────╮
│  ╭────╮                              │   card: surfaceContainer, r24, pad 20
│  │ 🖥 │  main-pc            ● Online │   icon block: primaryContainer r16
│  ╰────╯  v1.1.0 · Tailscale         │   status dot: green/amber/red + label
│                                      │
│  ▰▰▰▰▰▰▰▱▱▱   512 GB free of 1 TB    │   gauge: tertiaryContainer track,
│  /home                               │   tertiary fill, 8dp tall, r-full
│                                      │   (one row per root/drive, max 3,
│  ┌────────┐ ┌────────┐ ┌───┐         │    "+2 more" expands)
│  │ Browse │ │ Search │ │ ⋯ │         │   Browse=filled, Search=tonal,
│  └────────┘ └────────┘ └───┘         │   ⋯ menu: Transfers/Settings/Forget
╰──────────────────────────────────────╯
```
- **Offline host:** whole card content at 60% opacity except the name; status
  shows "Offline · last seen 2h ago" (from cached health); Browse still
  enabled (offline cache browsing already works).
- **Update available:** M3 banner *inside* the card under the gauge —
  `secondaryContainer`, "v1.7.0 available", tonal "Update" button. (Fold the
  update-dialog-Cancel bug fix in here — see backlog note in the plan.)
- **Empty state (no hosts):** centered illustration-free hero: large rounded
  `primaryContainer` circle with a `devices_rounded` icon, `headlineSmall`
  "Pair your first PC", `bodyMedium` one-liner, filled "Scan QR code" button.
- LAN vs Tailscale chip reflects the *currently active* address (the client
  knows which one it's using).

## 3. Explorer (Wave C)

### App bar + breadcrumbs
```
←  Documents                    ⌕  ☆  ⋮          ← titleLarge, actions rounded
〔 home 〕〉〔 zaid 〕〉〔 Documents 〕             ← scrollable chip row, 8 gap
```
- Breadcrumb chips: current = filled tonal, ancestors = outlined, deep paths
  collapse head into `〔 … 〕` menu chip. Long-press any crumb = copy path,
  with snackbar.

### Entry tile (list, comfortable density)
```
╭──╮   Vacation Photos                    ⋮      leading 40dp r12 container
│▣ │   12 items · Jun 8                          name titleMedium
╰──╯                                             meta bodySmall onSurfaceVariant
```
- Tile is borderless on `surface`; pressed/selected state paints a r16
  `primaryContainer` (selected) / ripple surface behind it. Star overlay badge
  on leading container for favorites.
- **Density setting** (mandatory, mitigates the direction's density risk):
  comfortable (above, ~72dp) / compact (single meta line inline after name,
  ~52dp, leading 32dp). Persisted per device in SharedPreferences, lives in
  the view-options popover with list/grid and sort.
- Thumbnails (images/videos) replace the icon in the same r12 container;
  video gets a small play glyph overlay.

### Grid cell
```
╭──────────╮     r16 thumbnail/icon area, 1:1
│    ▣     │     name below, titleSmall, 2 lines max
│          │     selected: 3dp primary border + check badge top-right
╰──────────╯
Vacation Ph…
```

### Selection mode
- Top bar morphs (fadeThrough 250ms): `✕  3 selected      ⊞ select all  ⋮`.
- Bottom contextual bar slides up: tonal surface `surfaceContainerHigh`, r28
  top, actions: Move / Copy / Share / Delete (delete in `error` color). Wave D's
  mini progress bar stacks above it when present.

### Destination picker (the big UX fix)
```
╭────────────────────────────────────────╮  modal sheet, r28 top, 90% height
│  Move 3 items to…                   ✕  │  headlineSmall
│  〔 home 〕〉〔 zaid 〕                    │  same breadcrumb chips as explorer
│  ──────────────────────────────────    │
│  ▣  Documents                       ›  │  folders only, same tile anatomy
│  ▣  Downloads                       ›  │
│  ▣  Pictures                        ›  │
│  ──────────────────────────────────    │
│  ＋ New folder            [ Move here ]│  filled confirm, disabled at origin
╰────────────────────────────────────────╯
```

### FAB / create
- FAB: `add_rounded`, primaryContainer, r16 (M3 large FAB style), opens a
  small menu sheet: New folder / New file / Upload here.
- Favorites: horizontal pin row at listing root — small tonal cards (icon +
  name, r16), max one row, overflow scrolls.

## 4. Transfers center (Wave D)

```
Active ─────────────────────────────
╭──────────────────────────────────────╮
│ ▤ video.mp4              ⏸     ✕    │  card surfaceContainer r16
│ ▰▰▰▰▰▰▱▱▱▱  62% · 14 MB/s · 0:42 left│  progress: primary, 6dp, r-full
│ → <home>/Videos                  │  bodySmall destination
╰──────────────────────────────────────╯
Queued (2) ▾    Done (5) ▾    Failed (1) ▾     collapsible labelLarge headers
```
- Swipe right = pause/resume (primaryContainer reveal), swipe left = remove
  (errorContainer reveal) with undo snackbar.
- Failed card: thin `errorContainer` strip with the error one-liner + tonal
  Retry button inline.
- Mini indicator in explorer: 3dp `LinearProgressIndicator` above the bottom
  bar, overall progress of active tasks; tap navigates here.

## 5. Preview (Wave E)

- Edge-to-edge content on `scrim`/black; top bar is a **gradient overlay**
  (transparent→40% scrim — gradient, not blur: Skia rule), auto-hides on tap.
- Top bar: `←  filename.jpg` + actions `share / save / delete / ⋮ (show in
  folder, details)`. Bottom center: `3 of 12` position pill,
  `surfaceContainerHigh` at 80% opacity, stadium.
- Swipe horizontally between previewable siblings (PageView). Hero from the
  tile thumbnail for images only.

## 6. Search & Settings alignment

- **Search** (already shipped in 1.7.0): keep behavior; Wave F aligns tokens —
  chips to stadium shape, result tiles adopt the unified `EntryTile`,
  highlight color switches to `primaryContainer`.
- **Settings:** group rows into tonal section cards (`surfaceContainer`, r24,
  16 pad): Connection / Storage & limits / Devices / Updates / About. Devices
  list uses entry-tile anatomy with a `errorContainer` revoke action.

## 7. States, a11y, do/don't

- **Loading:** keep existing skeletons; skeleton blocks r16 to match shape
  language, shimmer is opacity pulse (no shader shimmer — Skia).
- **Empty:** every list screen uses the same pattern as the dashboard empty
  hero (container circle + icon + one line + optional action). No sad-face
  illustrations.
- **Errors:** inline `errorContainer` banner with retry; full-screen error only
  when there is literally nothing cached to show.
- **A11y:** min 48dp targets (the ⋮ on tiles included), semantic labels on all
  icon-only buttons, test at 1.3× and 2.0× text scale (compact density must
  not overflow — let rows grow).
- **Don't:** shadows>level2, blurs, pure-black surfaces, ALL-CAPS labels, more
  than one accent block per card, font-size tweaks outside the type roles.

## 8. Wave mapping

| Wave | Implements from this spec |
|---|---|
| A | §1 foundations into `tokens.dart`/`app_theme.dart`, `EntryLeading`, type/shape/motion tokens |
| B | §2 dashboard |
| C | §3 explorer (incl. density setting + destination picker) |
| D | §4 transfers |
| E | §5 preview |
| F | dynamic color + light mode audit (§1), §6 alignment, §7 a11y pass |

Wave agents: when this spec and the plan conflict, the spec wins on visuals,
the plan wins on scope/sequencing. Cite the section you implemented in your
report.
