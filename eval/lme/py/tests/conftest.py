"""Test setup: import path, and a network guard that refuses every
non-loopback connection, so no test can ever reach a paid API."""

from __future__ import annotations

import os
import socket
import sys
import tempfile

import pytest

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, os.path.dirname(HERE))
sys.path.insert(0, HERE)

os.environ["MEM0_TELEMETRY"] = "False"
# Mem0 creates $MEM0_DIR (default ~/.mem0) at import: keep it out of $HOME.
os.environ["MEM0_DIR"] = tempfile.mkdtemp(prefix="mem0dir-")
os.environ.pop("DEEPSEEK_API_KEY", None)
os.environ.pop("OPENAI_API_KEY", None)

_real_connect = socket.socket.connect
_real_cx = socket.create_connection


def _loopback(addr) -> bool:
    if isinstance(addr, tuple):
        host = addr[0]
        return host in ("127.0.0.1", "::1", "localhost") or host.startswith("127.")
    return True  # AF_UNIX


def _guarded_connect(self, addr):
    if not _loopback(addr):
        raise ConnectionRefusedError(f"network guard: test tried to reach {addr!r}")
    return _real_connect(self, addr)


def _guarded_cx(address, *a, **kw):
    if not _loopback(address):
        raise ConnectionRefusedError(f"network guard: test tried to reach {address!r}")
    return _real_cx(address, *a, **kw)


socket.socket.connect = _guarded_connect
socket.create_connection = _guarded_cx


@pytest.fixture
def tmpdir_path(tmp_path):
    return str(tmp_path)
