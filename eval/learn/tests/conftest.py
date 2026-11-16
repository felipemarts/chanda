"""Test setup for eval/learn: import paths, and the same network guard as
eval/lme/py/tests (every non-loopback connection is refused, so no test can
reach a paid API)."""

from __future__ import annotations

import os
import socket
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
LEARN = os.path.dirname(HERE)
LMEPY = os.path.join(LEARN, "..", "lme", "py")
for p in (LMEPY, LEARN):
    sys.path.insert(0, p)

os.environ.pop("DEEPSEEK_API_KEY", None)

_real_connect = socket.socket.connect
_real_cx = socket.create_connection


def _loopback(addr) -> bool:
    if isinstance(addr, tuple):
        host = addr[0]
        return host in ("127.0.0.1", "::1", "localhost") or host.startswith("127.")
    return True


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
