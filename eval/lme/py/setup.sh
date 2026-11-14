#!/usr/bin/env bash
# Builds the pinned Python environment of eval/lme/py in the gitignored
# .local/ directory: uv 0.13.6 (checksum verified), CPython 3.12.16 managed
# by uv, and a venv synced from requirements.txt with --require-hashes.
# Nothing is installed system-wide. macOS arm64 or Linux x86_64/arm64.
set -euo pipefail
HERE="$(cd "$(dirname "$0")" && pwd)"
L="$HERE/.local"
UV_VERSION=0.13.6
PY_VERSION=3.12.16
case "$(uname -s)-$(uname -m)" in
  Darwin-arm64) T=aarch64-apple-darwin ;;
  Linux-x86_64) T=x86_64-unknown-linux-gnu ;;
  Linux-aarch64) T=aarch64-unknown-linux-gnu ;;
  *) echo "unsupported platform $(uname -s)-$(uname -m)" >&2; exit 1 ;;
esac
mkdir -p "$L/bin" "$L/dl"
if [ ! -x "$L/bin/uv" ] || ! "$L/bin/uv" --version | grep -q "^uv $UV_VERSION "; then
  ( cd "$L/dl"
    curl -fsSLO "https://github.com/astral-sh/uv/releases/download/$UV_VERSION/uv-$T.tar.gz"
    curl -fsSLO "https://github.com/astral-sh/uv/releases/download/$UV_VERSION/uv-$T.tar.gz.sha256"
    shasum -a 256 -c "uv-$T.tar.gz.sha256"
    tar xzf "uv-$T.tar.gz"
    cp "uv-$T/uv" "$L/bin/uv" )
fi
export UV_CACHE_DIR="$L/cache" UV_PYTHON_INSTALL_DIR="$L/python" UV_NO_CONFIG=1
"$L/bin/uv" python install "$PY_VERSION" --no-bin
[ -x "$L/venv/bin/python" ] || "$L/bin/uv" venv --python "$PY_VERSION" --managed-python "$L/venv"
VIRTUAL_ENV="$L/venv" "$L/bin/uv" pip sync --require-hashes "$HERE/requirements.txt"
"$L/venv/bin/python" -c 'import importlib.metadata as m, sys; print(sys.version.split()[0], "mem0ai", m.version("mem0ai"))'
