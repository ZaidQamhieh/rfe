// The pairing QR code (ui/qr.js, a small encoder with no dependencies). The expected matrices were
// decoded with an independent reader (zbarimg) when they were pinned: the text came back unchanged.
// A change that alters a pinned hash must be decoded again before the hash is updated.
//
//   node --test desktop/ui-tests/*.test.mjs

import { test } from "node:test";
import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { readFileSync } from "node:fs";
import vm from "node:vm";

const sandbox = {};
sandbox.window = sandbox;
vm.runInNewContext(readFileSync(new URL("../ui/qr.js", import.meta.url), "utf8"), sandbox);
const { QR } = sandbox;

const hash = (q) => {
  let s = "";
  for (let y = 0; y < q.size; y++) { for (let x = 0; x < q.size; x++) s += q.get(x, y) ? "1" : "0"; s += "\n"; }
  return q.size + " " + createHash("sha256").update(s).digest("hex").slice(0, 16);
};
const PAYLOAD = JSON.stringify({ address: "192.168.1.20:7443", tailscaleAddress: "100.101.4.7:7443", certFingerprint: "AB:CD:".repeat(10) + "AB", pairingCode: "482913" });

test("known inputs give the pinned matrices", () => {
  assert.equal(hash(QR.make("hello", "M")), "21 d9e93c47df2fd341");
  assert.equal(hash(QR.make(PAYLOAD, "M")), "53 b57487054670500e");
  assert.equal(hash(QR.make("x".repeat(100), "L")), "37 63123d538cc098d5");
});

test("the three finder patterns and the timing row are where the standard puts them", () => {
  const q = QR.make("hello", "M");
  const finder = (ox, oy) => { for (let y = 0; y < 7; y++) for (let x = 0; x < 7; x++) { const ring = Math.max(Math.abs(x - 3), Math.abs(y - 3)); assert.equal(q.get(ox + x, oy + y), ring !== 2, `finder at ${ox},${oy} module ${x},${y}`); } };
  finder(0, 0); finder(q.size - 7, 0); finder(0, q.size - 7);
  for (let i = 8; i < q.size - 8; i++) assert.equal(q.get(i, 6), i % 2 === 0, "timing row");
});

test("a payload that does not fit with the asked correction is encoded with a lower one", () => {
  const long = JSON.stringify({ address: "my-very-long-hostname.example.com:7443", tailscaleAddress: "my-pc.tail1234.ts.net:7443", certFingerprint: "AB:".repeat(31) + "AB", pairingCode: "482913" });
  assert.ok(long.length > 213 && long.length <= 271);
  assert.equal(QR.make(long, "M").size, 57);
});

test("a payload that fits nowhere is refused, not drawn wrongly", () => {
  assert.throws(() => QR.make("y".repeat(5000), "M"), /too long/);
});

test("the SVG is a single path with an accessible label", () => {
  const svg = QR.svg("hello", { label: "Pairing QR code for nas" });
  assert.match(svg, /^<svg [^>]*role="img"/);
  assert.match(svg, /aria-label="Pairing QR code for nas"/);
  assert.equal((svg.match(/<path/g) || []).length, 1);
});

test("the aria-label cannot break out of its attribute", () => {
  const svg = QR.svg("hello", { label: '"><meta http-equiv=refresh content=0>' });
  assert.ok(!svg.includes("<meta"), svg.slice(0, 400));
  assert.match(svg, /aria-label="&#34;&#62;&#60;meta/);
});
