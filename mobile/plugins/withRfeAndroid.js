// Reproducible native config for the RFE Android build (CNG; android/ is never hand-edited).
//  - RFE_DEBUG_KEYSTORE=<path>: sign debug/rehearsal builds with an existing keystore, so an
//    RN build can be installed over the Flutter debug build (same package + signature).
//  - RFE_RELEASE_KEYSTORE=<path> with RFE_RELEASE_STOREPASS_FILE=<path to a file holding the password> (and optionally
//    RFE_RELEASE_KEY_ALIAS, default "upload"): sign the release variant with the production key. The password is read
//    by Gradle at build time; it is never written into the generated project.
//  - RFE_TEST_BUILD=1: make the release variant debuggable for `adb run-as` verification.
//    Never set for real releases.
//  - allowBackup=false, as in the Flutter app: neither cloud backup nor adb backup may carry device keys or tokens.
//  - Network security config identical to the Flutter app's: cleartext is refused app-wide except
//    to 127.0.0.1, which the media loopback proxy needs (agent traffic stays pinned HTTPS).
const fs = require('fs');
const path = require('path');
const { withAndroidManifest, withAppBuildGradle, withDangerousMod } = require('expo/config-plugins');

const NETWORK_SECURITY_CONFIG = `<?xml version="1.0" encoding="utf-8"?>
<!-- Cleartext only to loopback, for the media proxy (MediaProxy.kt). Every agent request stays on
     the TLS-pinned HTTPS path, which this config does not touch. -->
<network-security-config>
    <domain-config cleartextTrafficPermitted="true">
        <domain includeSubdomains="false">127.0.0.1</domain>
    </domain-config>
    <base-config cleartextTrafficPermitted="false" />
</network-security-config>
`;

function withSigningAndDebuggable(config) {
  return withAppBuildGradle(config, (cfg) => {
    let g = cfg.modResults.contents;
    const ks = process.env.RFE_DEBUG_KEYSTORE;
    if (ks) {
      g = g.replace(/storeFile file\('debug\.keystore'\)/, `storeFile file(${JSON.stringify(ks)})`);
    }
    const relKs = process.env.RFE_RELEASE_KEYSTORE;
    const relPass = process.env.RFE_RELEASE_STOREPASS_FILE;
    // The key password defaults to the store password; CI keeps them as separate secrets.
    const relKeyPass = process.env.RFE_RELEASE_KEYPASS_FILE || relPass;
    if (relKs && relPass) {
      const alias = process.env.RFE_RELEASE_KEY_ALIAS || 'upload';
      const block = `        release {
            storeFile file(${JSON.stringify(relKs)})
            storePassword new File(${JSON.stringify(relPass)}).text.trim()
            keyAlias ${JSON.stringify(alias)}
            keyPassword new File(${JSON.stringify(relKeyPass)}).text.trim()
        }
`;
      if (!/signingConfigs \{/.test(g)) throw new Error('withRfeAndroid: no signingConfigs block to extend');
      g = g.replace(/(signingConfigs \{\n)/, `$1${block}`);
      // The release build type ships with the debug config in the Expo template; point it at the production key.
      g = g.replace(/(buildTypes \{[\s\S]*?release \{[\s\S]*?)signingConfig signingConfigs\.debug/, '$1signingConfig signingConfigs.release');
    } else if (relKs || relPass) {
      throw new Error('withRfeAndroid: set both RFE_RELEASE_KEYSTORE and RFE_RELEASE_STOREPASS_FILE');
    }
    // Release lint (lintVital) re-analyses every module for minutes and gates nothing the CI does not already check.
    if (!g.includes('checkReleaseBuilds')) {
      g = g.replace(/(\nandroid \{\n)/, '$1    lint {\n        checkReleaseBuilds false\n    }\n');
    }
    if (process.env.RFE_TEST_BUILD === '1' && !g.includes('debuggable true')) {
      // Anchored on buildTypes so the signing config's own `release {` is not the one that gets it (audit F-68).
      g = g.replace(/(buildTypes \{[\s\S]*?\n\s*release \{\n)/, '$1            debuggable true\n');
    }
    cfg.modResults.contents = g;
    return cfg;
  });
}

function withNetworkSecurityConfig(config) {
  config = withDangerousMod(config, [
    'android',
    (cfg) => {
      const dir = path.join(cfg.modRequest.platformProjectRoot, 'app/src/main/res/xml');
      fs.mkdirSync(dir, { recursive: true });
      fs.writeFileSync(path.join(dir, 'network_security_config.xml'), NETWORK_SECURITY_CONFIG);
      return cfg;
    },
  ]);
  return withAndroidManifest(config, (cfg) => {
    const app = cfg.modResults.manifest.application[0];
    app.$['android:networkSecurityConfig'] = '@xml/network_security_config';
    // The config above is the single source of truth for cleartext.
    delete app.$['android:usesCleartextTraffic'];
    // The device key, tokens and pins live here; the Flutter app opted out of backup and so does this one.
    app.$['android:allowBackup'] = 'false';
    return cfg;
  });
}

// FileProvider for handing single files to other apps (open with / share). Only cache/share and cache/open are exposed.
const FILE_PATHS = `<?xml version="1.0" encoding="utf-8"?>
<paths>
    <cache-path name="share" path="share/" />
    <cache-path name="open" path="open/" />
    <cache-path name="updates" path="updates/" />
</paths>
`;

function withFileProvider(config) {
  config = withDangerousMod(config, [
    'android',
    (cfg) => {
      const dir = path.join(cfg.modRequest.platformProjectRoot, 'app/src/main/res/xml');
      fs.mkdirSync(dir, { recursive: true });
      fs.writeFileSync(path.join(dir, 'rfe_file_paths.xml'), FILE_PATHS);
      return cfg;
    },
  ]);
  return withAndroidManifest(config, (cfg) => {
    const app = cfg.modResults.manifest.application[0];
    // The in-app updater hands a downloaded APK to the system installer.
    const perms = cfg.modResults.manifest['uses-permission'] ?? (cfg.modResults.manifest['uses-permission'] = []);
    if (!perms.some((p) => p.$['android:name'] === 'android.permission.REQUEST_INSTALL_PACKAGES')) {
      perms.push({ $: { 'android:name': 'android.permission.REQUEST_INSTALL_PACKAGES' } });
    }
    app.provider = (app.provider ?? []).filter((p) => p.$['android:authorities'] !== '${applicationId}.rfe.fileprovider');
    app.provider.push({
      $: {
        'android:name': 'androidx.core.content.FileProvider',
        'android:authorities': '${applicationId}.rfe.fileprovider',
        'android:exported': 'false',
        'android:grantUriPermissions': 'true',
      },
      'meta-data': [{ $: { 'android:name': 'android.support.FILE_PROVIDER_PATHS', 'android:resource': '@xml/rfe_file_paths' } }],
    });
    return cfg;
  });
}

module.exports = function withRfeAndroid(config) {
  return withFileProvider(withNetworkSecurityConfig(withSigningAndDebuggable(config)));
};
