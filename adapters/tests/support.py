"""Shared test setup: import paths and a stand-in ``chanda`` executable."""

from __future__ import annotations

import json
import os
import stat
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
ADAPTERS = os.path.dirname(HERE)
for p in (HERE, os.path.join(ADAPTERS, "generic"), os.path.join(ADAPTERS, "hermes")):
    if p not in sys.path:
        sys.path.insert(0, p)


def fake_chanda_command(tmpdir: str, scenario: str = "wired") -> tuple[str, dict[str, str], str]:
    """Write an executable that runs fake_chanda.py over stdio, standing in for
    the chanda binary. Returns (command, env for the child, call log path)."""
    log = os.path.join(tmpdir, "calls.jsonl")
    cmd = os.path.join(tmpdir, "chanda")
    with open(cmd, "w", encoding="utf-8") as f:
        f.write(f'#!/bin/sh\nexec "{sys.executable}" "{os.path.join(HERE, "fake_chanda.py")}" "$@"\n')
    os.chmod(cmd, os.stat(cmd).st_mode | stat.S_IXUSR)
    return cmd, {"FAKE_CHANDA_SCENARIO": scenario, "FAKE_CHANDA_LOG": log}, log


def read_log(path: str) -> list[dict]:
    if not os.path.exists(path):
        return []
    with open(path, encoding="utf-8") as f:
        return [json.loads(line) for line in f if line.strip()]
