#!/usr/bin/env python3
"""Capture a NotebookLM storage_state.json by driving a Chromium login.

Usage:
    python3 scripts/notebooklm/login.py --out /secrets/notebooklm-state.json

Opens a non-headless Chromium window. Sign in to your personal Google
account in that window, complete any 2FA challenge, navigate to
notebooklm.google.com, then return to this terminal and press Enter.
The captured cookies are written to ``--out`` with mode 600 on Unix.

The resulting file is what NOTEBOOKLM_STORAGE_STATE_PATH points at in the provider (NotebookLmAudioOverviewProvider).
"""

from __future__ import annotations

import argparse
import json
import os
import sys
from pathlib import Path
from urllib.parse import parse_qsl, urlencode, urlsplit, urlunsplit


def _with_authuser(url: str, authuser: str | None) -> str:
    if not authuser:
        return url
    parts = urlsplit(url)
    query = dict(parse_qsl(parts.query, keep_blank_values=True))
    query["authuser"] = authuser
    return urlunsplit(
        (parts.scheme, parts.netloc, parts.path, urlencode(query), parts.fragment)
    )


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument(
        "--out", required=True, help="Output path for storage_state.json"
    )
    parser.add_argument(
        "--url",
        default="https://notebooklm.google.com",
        help="URL to open after browser launch",
    )
    parser.add_argument(
        "--authuser",
        default=os.environ.get("NOTEBOOKLM_AUTHUSER", ""),
        help="Google account index to open, for example 1 for ?authuser=1.",
    )
    args = parser.parse_args()

    try:
        from playwright.sync_api import sync_playwright
    except ImportError as exc:
        print(
            "playwright not installed. Run `pip install -r scripts/notebooklm/login-requirements.txt`"
            f" and `playwright install chromium`. ({exc})",
            file=sys.stderr,
        )
        sys.exit(1)

    out = Path(args.out)
    out.parent.mkdir(parents=True, exist_ok=True)

    with sync_playwright() as p:
        browser = p.chromium.launch(headless=False)
        context = browser.new_context()
        page = context.new_page()
        page.goto(_with_authuser(args.url, args.authuser))
        print(
            "Sign in to your Google account in the opened browser, navigate to your NotebookLM dashboard,"
            " then return here and press Enter to save cookies..."
        )
        input()
        state = context.storage_state()
        with open(out, "w", encoding="utf-8") as f:
            json.dump(state, f)
        if os.name != "nt":
            os.chmod(out, 0o600)
        browser.close()

    print(f"saved storage state -> {out}")


if __name__ == "__main__":
    main()
