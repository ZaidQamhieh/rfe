// Shared by the tests that read the window's stylesheet text. The app's CSS is app.css (the colour tokens
// and the root size) plus the files ui/css/*.css that ui/index.html links after it. This returns them as one
// string in link order, which is the cascade order, so a test sees the same text whether the rules sit in
// one file or are spread over several. ui/base.css (the neutral reset, linked first) is not part of it, as
// before the split.

import { readFileSync } from "node:fs";

const ui = new URL("../ui/", import.meta.url);

/** The stylesheet paths (relative to ui/) that index.html links, in link order, starting at app.css. */
export function cssFiles() {
  const html = readFileSync(new URL("index.html", ui), "utf8");
  const hrefs = [...html.matchAll(/<link\b[^>]*\brel="stylesheet"[^>]*\bhref="([^"]+)"/g)].map((m) => m[1]);
  const at = hrefs.indexOf("app.css");
  if (at < 0) throw new Error("index.html does not link app.css");
  return hrefs.slice(at);
}

/** Like readAllCss() but without the v5 foundation sheets (css/v5-*.css, linked last), for tests that guard "the last labelled block of the app's own CSS". */
export function readLegacyCss() {
  return cssFiles().filter((f) => !/^css\/v5-/.test(f)).map((f) => readFileSync(new URL(f, ui), "utf8")).join("");
}

/** The text of app.css and every stylesheet linked after it from index.html, joined in link order. */
export function readAllCss() {
  return cssFiles().map((f) => readFileSync(new URL(f, ui), "utf8")).join("");
}
