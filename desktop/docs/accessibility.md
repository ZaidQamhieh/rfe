# Accessibility audit

Status 2026-10-05: keyboard-only runs of every sign-in route and the main actions pass in the real
window (`e2e/keyboard.test.mjs`), and the AT-SPI tree, which is what Orca reads, was checked
(`e2e/a11y.test.mjs`). **Not done:** a run with Orca itself (it is not installed on the test machine,
and speech output was not heard), and a check at a true 200% display scale (see below).

## What was found and fixed

| Finding | Fix |
|---|---|
| Tab left a dialog and went on through the page behind it | A dialog is modal: Tab and Shift+Tab stay inside it, and focus goes back to what opened it when it closes |
| Enter on a focused button in a dialog pressed the dialog's main button instead | Enter activates the focused button; it presses the main one only from a field |
| Switching tabs in the sign-in dialog dropped focus to the page | Focus goes to the field (or main button) of the new tab |
| Pressing Escape in the sign-in dialog left the approval request polling | Closing the dialog by any route stops the wait and cancels the request |
| Menus were not reachable by keyboard (items were plain `div`s) | A menu takes focus, Up, Down, Home and End move, Enter or Space chooses, Escape closes and gives focus back to its button, even if the page redrew meanwhile |
| A switch or a segmented button redrew the whole page and focus was lost | The same control gets focus back, so Space can be pressed again |
| Fields in dialogs had a visible label that was not tied to them | A `label` next to a field is tied to it (`for`), so the AT-SPI entry has the label's name |
| Errors and progress were both a polite `role="status"` | Errors are `role="alert"`; progress and results stay `status` |
| Form controls in the dark theme could keep light-theme defaults | `color-scheme: light dark` |

## What is checked automatically

- `ui-tests/window.test.mjs` (jsdom, also run in CI): every control has an accessible name, every screen
  draws with a heading and no script error, and the language switch flips the layout for Arabic.
- `ui-tests/contrast.test.mjs`: text, secondary text, accent, error, warning and success colours reach 4.5:1
  (WCAG AA) on their backgrounds in both themes, and outlines 3:1; the tokens are read from `app.css`.
- `e2e/keyboard.test.mjs` (real window): the tab order of the rail and top bar, the New connection
  dialog, the sign-in dialog and Settings is recorded and printed, every stop must show a focus outline
  (the search box shows it as the border of its bar), and the sign-in routes, switches, menus and the
  confirmation of Revoke are used with Tab, Enter, Space, the arrows and Escape.
- `e2e/a11y.test.mjs` (real window): reads the AT-SPI tree from a private accessibility bus. Buttons of
  the rail are named, a page has a heading, a dialog has the `dialog` role and a heading, its fields have
  their label as name, and a device row's menu button names its device.

## Already true from the structure

- DOM order is reading order, so the tab order is the visual order (rail, top bar, page). A "Skip to
  content" link is the first stop. Nothing sets a positive `tabindex`.
- Focus rings are the browser's `:focus-visible` outline in the accent colour, 2px with an offset.
- Destructive actions (forget a server, revoke or remove a device, create a new device key, delete) ask in
  a dialog that names what will happen.
- The pairing match code is plain text with an `aria-label`.
- Page changes are announced in a live region.

## Layout at small sizes

The window cannot be made smaller than 1100 by 700. The compositor's `--scale 2` did not change
`window.devicePixelRatio` (it stays 1), so a 200% run of `e2e/run.sh` is not a 200% run. A request for
what 200% of a 1280x800 screen comes to (640x400 CSS pixels) is checked instead: the window refuses to
get smaller than 1100x700, and `e2e/small-window.test.mjs` fails if any screen needs a sideways scroll at
that size.

## Still not done

1. Orca itself: each screen announced, errors interrupting, the match code read digit by digit.
2. A true 200% display scale (needs a real session; the remembered window size was also not driven:
   WebDriver's close ends the app without the close event the size is saved on). A second launch hands
   over and exits, covered by `e2e/window.test.mjs`.
3. The tables of the old window are gone; the lists of the new window (files, devices, servers) are built
   from `div`s with a name per row, and the AT-SPI test does not yet read their structure.

Record the result here, with the desktop environment and Orca version.
