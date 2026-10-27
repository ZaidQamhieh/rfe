// https://docs.expo.dev/guides/using-eslint/
const { defineConfig } = require('eslint/config');
const expoConfig = require('eslint-config-expo/flat');

// Layering, low to high: core (no UI, no app state) < state < design < features < app (routes).
// A layer may import only from the layers below it.
const above = (...layers) => layers.map((l) => ({ group: [`**/${l}`, `**/${l}/**`], message: `This layer must not import from ${l}/ (see the layering in eslint.config.js).` }));

module.exports = defineConfig([
  expoConfig,
  {
    // node_modules.*/ and android.*/ cover renamed leftover copies (e.g. node_modules.root-old) that are not source.
    ignores: ['dist/*', 'android/*', 'android.*/*', 'node_modules.*/*', 'ios/*', 'modules/*/android/build/*'],
  },
  {
    // Logging must go through core/log, which redacts tokens, pins and keys.
    files: ['src/**/*.{ts,tsx}'],
    rules: { 'no-console': 'error' },
  },
  { files: ['src/core/**/*.{ts,tsx}'], ignores: ['**/*.test.ts'], rules: { 'no-restricted-imports': ['error', { patterns: above('state', 'design', 'features', 'app') }] } },
  { files: ['src/state/**/*.{ts,tsx}'], ignores: ['**/*.test.ts'], rules: { 'no-restricted-imports': ['error', { patterns: above('design', 'features', 'app') }] } },
  { files: ['src/design/**/*.{ts,tsx}'], ignores: ['**/*.test.ts'], rules: { 'no-restricted-imports': ['error', { patterns: above('features', 'app') }] } },
  { files: ['src/features/**/*.{ts,tsx}'], ignores: ['**/*.test.ts'], rules: { 'no-restricted-imports': ['error', { patterns: above('app') }] } },
]);
