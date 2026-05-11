#!/usr/bin/env python3
"""NotebookLM audio overview bridge.

Drives the community-maintained `notebooklm-py` library against a personal
Google account whose session has already been captured in a
`storage_state.json` file (see `scripts/notebooklm/login.py`).

Inputs come from CLI flags; the resulting audio bytes are written to the
path passed via `--out`. A single-line JSON summary is printed to stdout so
the caller can pick up identifiers and timing.

Exit codes
  0  success — `--out` is a populated audio file
  1  configuration / argument error (missing file, missing dep)
  2  NotebookLM RPC / API failure
  3  unexpected error
"""

from __future__ import annotations

import argparse
import asyncio
import json
import os
import re
import shutil
import sys
import tempfile
import time
from pathlib import Path
from typing import NoReturn
from urllib.parse import parse_qsl, urlencode, urlparse, urlunparse


def _parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Generate a NotebookLM audio overview from a prompt.")
    parser.add_argument(
        "--probe",
        action="store_true",
        help="Cheap auth readiness check: load storage state, bootstrap auth tokens, exit 0 on success or 2 on auth failure. Skips notebook creation.",
    )
    parser.add_argument("--prompt", required=False, help="Source text fed to the notebook.")
    parser.add_argument("--out", required=False, help="Output file path for the audio overview bytes.")
    parser.add_argument(
        "--storage-state",
        required=True,
        help="Path to a NotebookLM storage_state.json captured via scripts/notebooklm/login.py.",
    )
    parser.add_argument("--source-title", default="Generation prompt", help="Title for the text source.")
    parser.add_argument("--notebook-title", default=None, help="Notebook title (defaults to the --out stem).")
    parser.add_argument("--instructions", default="", help="Style instructions for the audio overview generator.")
    parser.add_argument(
        "--audio-format",
        default="deep_dive",
        choices=["deep_dive", "brief", "critique", "debate"],
    )
    parser.add_argument(
        "--audio-length",
        default="default",
        choices=["short", "default", "long"],
    )
    parser.add_argument("--language", default="en")
    parser.add_argument(
        "--authuser",
        default=os.environ.get("NOTEBOOKLM_AUTHUSER", ""),
        help="Google account index to route NotebookLM requests through, for example 1 for ?authuser=1.",
    )
    parser.add_argument("--timeout", type=int, default=600, help="Overall timeout in seconds.")
    parser.add_argument("--poll-interval", type=int, default=5, help="Poll interval in seconds.")
    parser.add_argument(
        "--cleanup-notebook",
        action="store_true",
        help="Delete the notebook after the audio is downloaded.",
    )
    return parser.parse_args()


def _fail(code: int, message: str) -> NoReturn:
    print(message, file=sys.stderr)
    sys.exit(code)


def _copy_storage_state_to_writable_temp(storage: Path) -> tuple[Path, tempfile.TemporaryDirectory]:
    temp_dir = tempfile.TemporaryDirectory(prefix="notebooklm-state-")
    temp_path = Path(temp_dir.name) / "state.json"
    shutil.copyfile(storage, temp_path)
    os.chmod(temp_path, 0o600)
    return temp_path, temp_dir


def _install_authuser_patch(authuser: str | None) -> None:
    """Route notebooklm-py calls through the selected Google account index.

    notebooklm-py 0.4.0 does not expose an authuser knob and its decoder warns
    that multi-account browser sessions default to account index 0. Keep this
    patch local to the bridge so the rest of the repo does not depend on a
    forked private-API client.
    """
    if not authuser:
        return
    account = authuser.strip()
    if not account.isdigit():
        _fail(1, f"--authuser must be a non-negative integer, got {authuser!r}")

    import httpx
    import notebooklm.auth as auth_mod
    from notebooklm._artifacts import ArtifactsAPI
    from notebooklm._core import ClientCore
    from notebooklm._url_utils import is_google_auth_redirect
    from notebooklm.client import NotebookLMClient
    from notebooklm.rpc import BATCHEXECUTE_URL

    home_url = f"https://notebooklm.google.com/?authuser={account}"

    async def _fetch_tokens_with_jar(cookie_jar):
        async with httpx.AsyncClient(cookies=cookie_jar) as client:
            response = await client.get(home_url, follow_redirects=True, timeout=30.0)
            response.raise_for_status()
            final_url = str(response.url)
            if is_google_auth_redirect(final_url):
                raise ValueError(
                    "Authentication expired or invalid for "
                    f"notebooklm.google.com/?authuser={account}. Run make notebooklm-import."
                )
            csrf = auth_mod.extract_csrf_from_html(response.text, final_url)
            session_id = auth_mod.extract_session_id_from_html(response.text, final_url)
            auth_mod._replace_cookie_jar(cookie_jar, client.cookies)
            return csrf, session_id

    def _build_url(self, rpc_method, source_path="/"):
        params = {
            "rpcids": rpc_method.value,
            "source-path": source_path,
            "f.sid": self.auth.session_id,
            "authuser": account,
            "rt": "c",
        }
        return f"{BATCHEXECUTE_URL}?{urlencode(params)}"

    original_download_url = ArtifactsAPI._download_url

    def _with_authuser(url: str) -> str:
        parsed = urlparse(url)
        if not parsed.netloc.endswith("googleusercontent.com"):
            return url
        query = parse_qsl(parsed.query, keep_blank_values=True)
        if any(key == "authuser" for key, _ in query):
            return url
        query.append(("authuser", account))
        return urlunparse(parsed._replace(query=urlencode(query)))

    async def _download_url(self, url, output_path):
        return await original_download_url(self, _with_authuser(url), output_path)

    async def _refresh_auth(self):
        http_client = self._core.get_http_client()
        response = await http_client.get(home_url)
        response.raise_for_status()
        final_url = str(response.url)
        if is_google_auth_redirect(final_url):
            raise ValueError(
                "Authentication expired for "
                f"notebooklm.google.com/?authuser={account}. Run make notebooklm-import."
            )

        csrf_match = re.search(r'"SNlM0e":"([^"]+)"', response.text)
        if not csrf_match:
            raise ValueError("Failed to extract CSRF token (SNlM0e).")
        self._core.auth.csrf_token = csrf_match.group(1)

        sid_match = re.search(r'"FdrFJe":"([^"]+)"', response.text)
        if not sid_match:
            raise ValueError("Failed to extract session ID (FdrFJe).")
        self._core.auth.session_id = sid_match.group(1)

        self._core.update_auth_headers()
        auth_mod.save_cookies_to_storage(http_client.cookies, self._core.auth.storage_path)
        return self._core.auth

    auth_mod._fetch_tokens_with_jar = _fetch_tokens_with_jar
    ClientCore._build_url = _build_url
    ArtifactsAPI._download_url = _download_url
    NotebookLMClient.refresh_auth = _refresh_auth


def main() -> None:
    args = _parse_args()

    storage = Path(args.storage_state)
    if not storage.is_file():
        _fail(1, f"storage_state.json not found at {storage}")
    storage_for_client, storage_temp_dir = _copy_storage_state_to_writable_temp(storage)

    try:
        from notebooklm import NotebookLMClient, AudioFormat, AudioLength
    except ImportError as exc:
        _fail(1, f"notebooklm-py not installed: {exc}. Run `pip install -r scripts/notebooklm/requirements.txt`.")

    _install_authuser_patch(args.authuser)

    if args.probe:
        async def _probe() -> None:
            # Opening the client triggers _fetch_tokens_with_jar which raises ValueError
            # "Authentication expired" when Google redirects to accounts.google.com.
            async with await NotebookLMClient.from_storage(str(storage_for_client)) as _client:
                return None
        try:
            asyncio.run(_probe())
            sys.exit(0)
        except Exception as exc:
            _fail(2, f"notebooklm error: {exc}")
        finally:
            storage_temp_dir.cleanup()

    if not args.prompt:
        _fail(1, "--prompt is required unless --probe is set")
    if not args.out:
        _fail(1, "--out is required unless --probe is set")

    audio_format_map = {
        "deep_dive": AudioFormat.DEEP_DIVE,
        "brief": AudioFormat.BRIEF,
        "critique": AudioFormat.CRITIQUE,
        "debate": AudioFormat.DEBATE,
    }
    audio_length_map = {
        "short": AudioLength.SHORT,
        "default": AudioLength.DEFAULT,
        "long": AudioLength.LONG,
    }

    async def _run() -> dict:
        async with await NotebookLMClient.from_storage(str(storage_for_client)) as client:
            nb = None
            summary = None
            title = args.notebook_title or Path(args.out).stem or "Audio overview"
            try:
                nb = await client.notebooks.create(title)
                await client.sources.add_text(nb.id, args.source_title, args.prompt, wait=True)
                status = await client.artifacts.generate_audio(
                    nb.id,
                    instructions=args.instructions or "",
                    audio_format=audio_format_map[args.audio_format],
                    audio_length=audio_length_map[args.audio_length],
                    language=args.language,
                )
                final = await client.artifacts.wait_for_completion(
                    notebook_id=nb.id,
                    task_id=status.task_id,
                    timeout=args.timeout,
                    initial_interval=args.poll_interval,
                )
                if not getattr(final, "is_complete", False):
                    raise RuntimeError(
                        f"audio overview did not complete: status={getattr(final, 'status', 'unknown')}"
                    )
                output_path = await client.artifacts.download_audio(nb.id, args.out)
                summary = {
                    "output": str(output_path),
                    "notebook_id": nb.id,
                    "task_id": status.task_id,
                    "audio_format": args.audio_format,
                    "audio_length": args.audio_length,
                    "language": args.language,
                }
                if args.authuser:
                    summary["authuser"] = args.authuser
                return summary
            finally:
                if args.cleanup_notebook and nb is not None:
                    cleanup = "deleted"
                    try:
                        await client.notebooks.delete(nb.id)
                    except Exception as cleanup_err:
                        cleanup = f"failed: {cleanup_err}"
                    if summary is not None:
                        summary["cleanup"] = cleanup
                    else:
                        print(f"cleanup={cleanup}", file=sys.stderr)

    start = time.monotonic()
    try:
        try:
            result = asyncio.run(_run())
        except KeyboardInterrupt:
            _fail(2, "interrupted")
        except ImportError as exc:
            _fail(1, f"notebooklm-py import failed: {exc}")
        except Exception as exc:
            _fail(2, f"notebooklm error: {exc}")

        result["elapsed_ms"] = int((time.monotonic() - start) * 1000)
        json.dump(result, sys.stdout)
        sys.stdout.write("\n")
    finally:
        storage_temp_dir.cleanup()


if __name__ == "__main__":
    main()
