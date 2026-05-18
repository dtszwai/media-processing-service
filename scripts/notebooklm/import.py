#!/usr/bin/env python3
"""Import NotebookLM cookies from an exact Chrome profile.

The upstream ``notebooklm login --browser-cookies`` path reads a browser-level
cookie store. That is risky on a machine with multiple Chrome profiles because
it can silently import the wrong Google account. This script pins the Chrome
profile directory and validates the expected account position before writing
``storage_state.json``.
"""

from __future__ import annotations

import argparse
import asyncio
import json
import os
import sys
import tempfile
from pathlib import Path
from typing import Any, NoReturn


def _fail(message: str) -> NoReturn:
    print(message, file=sys.stderr)
    sys.exit(1)


def _parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Import NotebookLM auth from Chrome cookies."
    )
    parser.add_argument(
        "--out", required=True, help="Output path for storage_state.json"
    )
    parser.add_argument(
        "--browser",
        default="chrome",
        choices=["chrome"],
        help="Browser to import from.",
    )
    parser.add_argument(
        "--chrome-profile",
        default="Default",
        help="Chrome profile directory, e.g. Default.",
    )
    parser.add_argument(
        "--authuser",
        default="1",
        help="Expected Google authuser index in that profile.",
    )
    parser.add_argument(
        "--expected-email",
        default="",
        help="Optional expected email at the authuser index, e.g. john.doe@gmail.com.",
    )
    parser.add_argument(
        "--chrome-user-data-dir",
        default="",
        help="Override Chrome user data directory. Defaults to ~/Library/Application Support/Google/Chrome.",
    )
    return parser.parse_args()


def _chrome_user_data_dir(arg: str) -> Path:
    if arg:
        return Path(arg).expanduser()
    if sys.platform == "darwin":
        return Path.home() / "Library/Application Support/Google/Chrome"
    if sys.platform.startswith("linux"):
        return Path.home() / ".config/google-chrome"
    if sys.platform == "win32":
        local_app_data = os.environ.get("LOCALAPPDATA")
        if local_app_data:
            return Path(local_app_data) / "Google/Chrome/User Data"
    _fail(
        "Unsupported platform for automatic Chrome profile discovery. Pass --chrome-user-data-dir."
    )


def _load_preferences(profile_dir: Path) -> dict[str, Any]:
    preferences = profile_dir / "Preferences"
    if not preferences.is_file():
        _fail(f"Chrome Preferences not found at {preferences}")
    try:
        return json.loads(preferences.read_text(encoding="utf-8"))
    except json.JSONDecodeError as exc:
        _fail(f"Chrome Preferences is not valid JSON: {exc}")


def _validate_expected_account(
    profile_dir: Path, authuser: str, expected_email: str
) -> None:
    if not authuser.isdigit():
        _fail(f"--authuser must be a non-negative integer, got {authuser!r}")

    preferences = _load_preferences(profile_dir)
    accounts = preferences.get("account_info") or []
    index = int(authuser)
    if index >= len(accounts):
        emails = [account.get("email", "<unknown>") for account in accounts]
        _fail(
            f"Chrome profile {profile_dir.name!r} has {len(accounts)} Google account(s); "
            f"authuser={authuser} is out of range. Accounts: {emails}"
        )

    email = accounts[index].get("email", "")
    if expected_email and email.lower() != expected_email.lower():
        _fail(
            f"Chrome profile {profile_dir.name!r} authuser={authuser} is {email!r}, "
            f"expected {expected_email!r}."
        )


def _fsync_dir(path: Path) -> None:
    if os.name == "nt":
        return
    fd = os.open(path, os.O_RDONLY)
    try:
        os.fsync(fd)
    finally:
        os.close(fd)


def _atomic_write_text(path: Path, text: str) -> None:
    fd, tmp = tempfile.mkstemp(
        dir=path.parent,
        prefix=f"{path.name}.",
        suffix=".tmp",
        text=True,
    )
    tmp_path = Path(tmp)
    try:
        with os.fdopen(fd, "w", encoding="utf-8") as handle:
            handle.write(text)
            handle.flush()
            os.fsync(handle.fileno())
        if os.name != "nt":
            tmp_path.chmod(0o600)
        os.replace(tmp_path, path)
        _fsync_dir(path.parent)
    except Exception:
        try:
            tmp_path.unlink()
        except FileNotFoundError:
            pass
        raise


def _atomic_save_cookies_to_storage(jar: Any, path: Path) -> None:
    from notebooklm.auth import save_cookies_to_storage

    existing = path.read_bytes()
    fd, tmp = tempfile.mkstemp(
        dir=path.parent,
        prefix=f"{path.name}.",
        suffix=".tmp",
    )
    os.close(fd)
    tmp_path = Path(tmp)
    try:
        tmp_path.write_bytes(existing)
        save_cookies_to_storage(jar, tmp_path)
        if os.name != "nt":
            tmp_path.chmod(0o600)
        with tmp_path.open("rb") as handle:
            os.fsync(handle.fileno())
        os.replace(tmp_path, path)
        _fsync_dir(path.parent)
    except Exception:
        try:
            tmp_path.unlink()
        except FileNotFoundError:
            pass
        raise


async def _verify_storage(path: Path, authuser: str) -> None:
    import httpx
    from notebooklm._url_utils import is_google_auth_redirect
    from notebooklm.auth import (
        build_httpx_cookies_from_storage,
        extract_csrf_from_html,
        extract_session_id_from_html,
    )

    jar = build_httpx_cookies_from_storage(path)
    url = f"https://notebooklm.google.com/?authuser={authuser}"
    async with httpx.AsyncClient(cookies=jar) as client:
        response = await client.get(url, follow_redirects=True, timeout=30.0)
        response.raise_for_status()
        final_url = str(response.url)
        if is_google_auth_redirect(final_url):
            _fail(
                f"Imported cookies redirect to Google sign-in for {url}. Open Chrome and sign in first."
            )
        extract_csrf_from_html(response.text, final_url)
        extract_session_id_from_html(response.text, final_url)
        jar.jar.clear()
        for cookie in client.cookies.jar:
            jar.jar.set_cookie(cookie)
    _atomic_save_cookies_to_storage(jar, path)


def main() -> None:
    args = _parse_args()

    try:
        import rookiepy
        from notebooklm.auth import (
            convert_rookiepy_cookies_to_storage_state,
            extract_cookies_from_storage,
        )
        from notebooklm.cli.session import (
            ALLOWED_COOKIE_DOMAINS,
            GOOGLE_REGIONAL_CCTLDS,
        )
    except ImportError as exc:
        _fail(f"Missing NotebookLM login dependency: {exc}. Run make notebooklm-venv.")

    profile_dir = _chrome_user_data_dir(args.chrome_user_data_dir) / args.chrome_profile
    cookies_db = profile_dir / "Cookies"
    if not cookies_db.is_file():
        _fail(f"Chrome Cookies database not found at {cookies_db}")

    _validate_expected_account(profile_dir, args.authuser, args.expected_email)

    domains = list(ALLOWED_COOKIE_DOMAINS)
    for cctld in GOOGLE_REGIONAL_CCTLDS:
        domain = f".google.{cctld}"
        if domain not in domains:
            domains.append(domain)

    try:
        raw_cookies = rookiepy.any_browser(str(cookies_db), domains=domains)
    except (OSError, RuntimeError) as exc:
        _fail(f"Could not read Chrome cookies from {cookies_db}: {exc}")

    storage_state = convert_rookiepy_cookies_to_storage_state(raw_cookies)
    try:
        extract_cookies_from_storage(storage_state)
    except ValueError as exc:
        _fail(f"No valid Google authentication cookies found in {profile_dir}: {exc}")

    out = Path(args.out).expanduser()
    out.parent.mkdir(parents=True, exist_ok=True)
    if os.name != "nt":
        out.parent.chmod(0o700)
    _atomic_write_text(out, json.dumps(storage_state, indent=2, ensure_ascii=False))
    if os.name != "nt":
        out.chmod(0o600)

    asyncio.run(_verify_storage(out, args.authuser))
    try:
        json.load(out.open("r", encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as exc:
        _fail(f"Imported NotebookLM state could not be read back from {out}: {exc}")

    email = args.expected_email or f"authuser={args.authuser}"
    print(
        f"Imported NotebookLM cookies from Chrome profile {args.chrome_profile!r} "
        f"for {email} into {out}"
    )


if __name__ == "__main__":
    main()
