#!/usr/bin/env python3
"""Load ArchiMate Lite 2.2.0 demo instance (and metamodel dependencies)."""

from __future__ import annotations

import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from _lib.load_catalog import main  # noqa: E402

if __name__ == "__main__":
    raise SystemExit(main(Path(__file__).resolve().parent))
