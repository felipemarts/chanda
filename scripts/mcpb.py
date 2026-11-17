#!/usr/bin/env python3
"""Build one MCP bundle (.mcpb) per platform from the `make release` binaries,
and the server.json the MCP Registry publishes (decision 0150).

usage: python3 scripts/mcpb.py v0.1.0 [dist]

Reads dist/chanda-<version>-<os>-<arch>[.exe] and packaging/mcpb/, writes
dist/chanda-mcp-<os>-<arch>.mcpb (the registry wants "mcp" in the file name)
and dist/server.json. Packing uses the official CLI through npx.
"""
import hashlib
import json
import os
import re
import shutil
import subprocess
import sys

REPO = "https://github.com/felipemarts/chanda"
MCPB_CLI = "@anthropic-ai/mcpb@2.1.2"
PLATFORM = {"linux": "linux", "darwin": "darwin", "windows": "win32"}
HERE = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))


def main(tag, dist):
    version = tag.lstrip("v")
    manifest = json.load(open(os.path.join(HERE, "packaging/mcpb/manifest.json"), encoding="utf-8"))
    pattern = re.compile(rf"^chanda-{re.escape(tag)}-(\w+)-(\w+?)(\.exe)?$")
    packages = []
    for name in sorted(os.listdir(dist)):
        m = pattern.match(name)
        if not m:
            continue
        goos, goarch, ext = m.group(1), m.group(2), m.group(3) or ""
        stage = os.path.join(dist, "mcpb", f"{goos}-{goarch}")
        shutil.rmtree(stage, ignore_errors=True)
        os.makedirs(os.path.join(stage, "server"))
        exe = "server/chanda" + ext
        shutil.copy2(os.path.join(dist, name), os.path.join(stage, exe))
        os.chmod(os.path.join(stage, exe), 0o755)
        shutil.copy2(os.path.join(HERE, "packaging/mcpb/icon.png"), stage)
        mf = json.loads(json.dumps(manifest))
        mf["version"] = version
        mf["server"]["entry_point"] = exe
        mf["server"]["mcp_config"]["command"] = "${__dirname}/" + exe
        mf["compatibility"]["platforms"] = [PLATFORM[goos]]
        with open(os.path.join(stage, "manifest.json"), "w", encoding="utf-8") as f:
            json.dump(mf, f, indent=2)
        bundle = f"chanda-mcp-{goos}-{goarch}.mcpb"
        out = os.path.join(dist, bundle)
        npx = "npx.cmd" if os.name == "nt" else "npx"
        subprocess.run([npx, "-y", MCPB_CLI, "pack", stage, out], check=True)
        sha = hashlib.sha256(open(out, "rb").read()).hexdigest()
        packages.append({
            "registryType": "mcpb",
            "identifier": f"{REPO}/releases/download/{tag}/{bundle}",
            "fileSha256": sha,
            "transport": {"type": "stdio"},
        })
        print(f"{bundle}  {sha}")
    if not packages:
        sys.exit(f"no binaries for {tag} in {dist}; run make release first")
    server = {
        "$schema": "https://static.modelcontextprotocol.io/schemas/2025-12-11/server.schema.json",
        "name": "io.github.felipemarts/chanda",
        "title": "Chanda",
        "description": "Local long-term memory for AI agents: hybrid search, temporal facts, verifiable forgetting.",
        "websiteUrl": REPO,
        "repository": {"url": REPO, "source": "github"},
        "version": version,
        "packages": packages,
    }
    with open(os.path.join(dist, "server.json"), "w", encoding="utf-8") as f:
        json.dump(server, f, indent=2)
        f.write("\n")
    shutil.rmtree(os.path.join(dist, "mcpb"))


if __name__ == "__main__":
    main(sys.argv[1], sys.argv[2] if len(sys.argv) > 2 else "dist")
