// Builds src/features/support/licenses.json for the packages that actually ship in the app bundle: a production export
// is made, its source map names every node_modules package that was bundled, and each package's name, version, SPDX
// license and license text is read from node_modules. Identical texts are stored once. Run after changing dependencies:
//   node scripts/gen-licenses.mjs
import { execFileSync } from 'node:child_process';
import { createHash } from 'node:crypto';
import { existsSync, mkdtempSync, readdirSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

const out = mkdtempSync(join(tmpdir(), 'rfe-licenses-'));
try {
  execFileSync('npx', ['expo', 'export', '--platform', 'android', '--output-dir', out, '--source-maps'], { stdio: 'inherit' });
  const jsDir = join(out, '_expo/static/js/android');
  const mapFile = readdirSync(jsDir).find((f) => f.endsWith('.map'));
  const map = JSON.parse(readFileSync(join(jsDir, mapFile), 'utf8'));

  const dirs = new Set();
  for (const src of map.sources) {
    const i = src.lastIndexOf('node_modules/');
    if (i < 0) continue;
    const rest = src.slice(i + 'node_modules/'.length).split('/');
    const name = rest[0].startsWith('@') ? `${rest[0]}/${rest[1]}` : rest[0];
    dirs.add(src.slice(0, i + 'node_modules/'.length) + name);
  }

  const texts = {};
  const packages = [];
  const seen = new Set();
  for (const rel of dirs) {
    const dir = join(process.cwd(), rel.replace(/^(\.\.\/)+/, ''));
    const pjson = join(dir, 'package.json');
    if (!existsSync(pjson)) continue;
    const pkg = JSON.parse(readFileSync(pjson, 'utf8'));
    const key = `${pkg.name}@${pkg.version}`;
    if (seen.has(key)) continue;
    seen.add(key);
    const license = typeof pkg.license === 'string' ? pkg.license : pkg.license?.type ?? (Array.isArray(pkg.licenses) ? pkg.licenses.map((l) => l.type).join(' OR ') : 'UNKNOWN');
    const file = readdirSync(dir).find((f) => /^(licen[sc]e|copying)(\..*)?$/i.test(f));
    let hash = null;
    if (file) {
      const text = readFileSync(join(dir, file), 'utf8').replace(/\r\n/g, '\n').trim();
      hash = createHash('sha1').update(text).digest('hex').slice(0, 10);
      texts[hash] = text;
    }
    packages.push({ n: pkg.name, v: pkg.version, l: license, t: hash });
  }
  packages.sort((a, b) => a.n.localeCompare(b.n));
  writeFileSync('src/features/support/licenses.json', JSON.stringify({ packages, texts }));
  console.log(`${packages.length} packages, ${Object.keys(texts).length} distinct texts, ${packages.filter((p) => p.t === null).length} without a license file`);
} finally {
  rmSync(out, { recursive: true, force: true });
}
