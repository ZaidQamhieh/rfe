#!/usr/bin/env python3
"""Writes a CycloneDX 1.5 SBOM (JSON) for the desktop app from its Cargo.lock.

    desktop/scripts/sbom.py desktop/src-tauri/Cargo.lock out.json

Every crate in the lock file is a component, with its package URL and, for crates from a
registry, the SHA-256 the lock file records for the downloaded source. The app itself is the
described component. Nothing here is fetched: the lock file is the whole input, so the same
lock file always gives the same SBOM apart from the timestamp.
"""
import json
import sys
import tomllib
from datetime import datetime, timezone


def purl(name, version):
    return f"pkg:cargo/{name}@{version}"


def main(lock_path, out_path):
    with open(lock_path, "rb") as f:
        lock = tomllib.load(f)
    packages = lock.get("package", [])

    # The app is the one package that has no source (a path crate) and is named rfe-desktop.
    root = next(p for p in packages if p["name"] == "rfe-desktop")

    by_name = {}
    for p in packages:
        by_name.setdefault(p["name"], []).append(p)

    def ref(p):
        return purl(p["name"], p["version"])

    def dep_ref(entry):
        # "name" when only one version exists, "name version" or "name version (source)" otherwise.
        parts = entry.split(" ")
        name = parts[0]
        version = parts[1] if len(parts) > 1 else by_name[name][0]["version"]
        return purl(name, version)

    components = []
    dependencies = []
    for p in sorted(packages, key=lambda q: (q["name"], q["version"])):
        deps = sorted({dep_ref(d) for d in p.get("dependencies", [])})
        dependencies.append({"ref": ref(p), "dependsOn": deps})
        if p is root:
            continue
        c = {
            "type": "library",
            "bom-ref": ref(p),
            "name": p["name"],
            "version": p["version"],
            "purl": ref(p),
        }
        if "checksum" in p:
            c["hashes"] = [{"alg": "SHA-256", "content": p["checksum"]}]
        components.append(c)

    bom = {
        "bomFormat": "CycloneDX",
        "specVersion": "1.5",
        "version": 1,
        "metadata": {
            "timestamp": datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ"),
            "component": {
                "type": "application",
                "bom-ref": ref(root),
                "name": root["name"],
                "version": root["version"],
                "purl": ref(root),
            },
        },
        "components": components,
        "dependencies": dependencies,
    }
    with open(out_path, "w") as f:
        json.dump(bom, f, indent=2)
        f.write("\n")


if __name__ == "__main__":
    if len(sys.argv) != 3:
        sys.exit(__doc__)
    main(sys.argv[1], sys.argv[2])
