"""CLI entry point."""

from __future__ import annotations

import argparse
import sys
from datetime import datetime
from pathlib import Path

from onf import __version__
from onf.config import CaptureMode, ChromeConfig, RunConfig
from onf.logging_utils import log_info
from onf.paths import configure_frozen_runtime, default_output_dir, is_frozen
from onf.runner import run_capture

DEFAULT_OUTPUT = "./captures"


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(
        prog="onf",
        description=(
            "Own Network Fetcher — capture browser sessions to JSON. "
            "Default mode records cookie traffic only."
        ),
    )
    parser.add_argument("--version", action="version", version=f"onf {__version__}")

    mode = parser.add_mutually_exclusive_group()
    mode.add_argument(
        "--all-requests",
        action="store_true",
        help="Record all network requests/responses (default: cookie-only mode).",
    )

    parser.add_argument(
        "--output-dir",
        default=DEFAULT_OUTPUT,
        help="Directory for session output (default: ./captures or next to onf.exe).",
    )
    parser.add_argument(
        "--chrome-host",
        default="127.0.0.1",
        help="Chrome remote debugging host (default: 127.0.0.1).",
    )
    parser.add_argument(
        "--chrome-port",
        type=int,
        default=9222,
        help="Chrome remote debugging port (default: 9222).",
    )
    parser.add_argument(
        "--task-id",
        default=None,
        help="Task identifier (default: auto-generated timestamp).",
    )
    parser.add_argument(
        "--include-sensitive",
        action="store_true",
        help="Include sensitive headers in full mode (authorization, api keys).",
    )
    parser.add_argument(
        "--flush-interval",
        type=float,
        default=2.0,
        help="Seconds between session.json refreshes (default: 2).",
    )
    parser.add_argument(
        "--no-pause",
        action="store_true",
        help="Do not wait for Enter before closing (CMD scripts).",
    )
    return parser


def print_startup_banner(config: RunConfig) -> None:
    mode = "cookie-only" if config.cookie_only else "full (all requests)"
    log_info("=" * 56)
    log_info(f"Own Network Fetcher v{__version__}")
    log_info(f"Capture mode : {mode}")
    log_info(f"Chrome CDP   : {config.chrome.cdp_url}")
    log_info(f"Task ID      : {config.task_id}")
    log_info(f"Output folder: {config.session_dir}")
    log_info("-" * 56)
    log_info("Pehle Chrome debug mode mein kholo (ya already open rakho):")
    log_info(
        f'  scripts\\start_chrome_debug.bat   (recommended)\n'
        f'  OR chrome.exe --remote-debugging-port={config.chrome.port} '
        "--remote-allow-origins=* --user-data-dir=%LOCALAPPDATA%\\onf-chrome-debug"
    )
    log_info("Phir Chrome mein site browse karo. Band karne ke liye: Ctrl+C")
    log_info("=" * 56)


def resolve_output_dir(raw: str) -> Path:
    if is_frozen() and raw in {DEFAULT_OUTPUT, ".\\captures", "./captures"}:
        return default_output_dir()
    return Path(raw)


def build_config(args: argparse.Namespace) -> RunConfig:
    capture_mode = CaptureMode.FULL if args.all_requests else CaptureMode.COOKIE_ONLY
    task_id = args.task_id or datetime.now().strftime("task_%Y%m%d_%H%M%S")

    return RunConfig(
        task_id=task_id,
        output_dir=resolve_output_dir(args.output_dir),
        chrome=ChromeConfig(host=args.chrome_host, port=args.chrome_port),
        capture_mode=capture_mode,
        include_sensitive=args.include_sensitive,
        flush_interval_s=args.flush_interval,
    )


def should_pause(*, disabled: bool) -> bool:
    if disabled:
        return False
    if is_frozen():
        return True
    return sys.stdin.isatty()


def pause_before_exit(exit_code: int, *, disabled: bool) -> None:
    if not should_pause(disabled=disabled):
        return
    try:
        input("\nPress Enter to exit...")
    except EOFError:
        pass
    raise SystemExit(exit_code)


def main(argv: list[str] | None = None) -> int:
    configure_frozen_runtime()

    cli_argv = list(sys.argv[1:] if argv is None else argv)
    double_click = is_frozen() and not cli_argv

    args = build_parser().parse_args(cli_argv)
    config = build_config(args)
    print_startup_banner(config)

    if double_click:
        log_info("Double-click mode: default cookie-only capture started.")

    exit_code = 0
    try:
        exit_code = run_capture(config)
    except Exception as exc:
        log_info(f"Error: {exc}")
        exit_code = 1

    if exit_code != 0:
        log_info(f"Exit code: {exit_code}")

    pause_before_exit(exit_code, disabled=args.no_pause)
    return exit_code


if __name__ == "__main__":
    raise SystemExit(main())
