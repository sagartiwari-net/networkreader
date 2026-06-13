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
            "Default mode exports cookies + storage on stop."
        ),
    )
    parser.add_argument("--version", action="version", version=f"onf {__version__}")

    mode = parser.add_mutually_exclusive_group()
    mode.add_argument(
        "--full-network",
        action="store_true",
        help="Option 1: full network scan with detailed request/response per site.",
    )
    mode.add_argument(
        "--cookie-export",
        action="store_true",
        help="Option 2: cookie scan only (HTTP + localStorage + IndexedDB on stop).",
    )
    mode.add_argument(
        "--all-requests",
        action="store_true",
        help=argparse.SUPPRESS,
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
    if config.full_network:
        mode = "full network scan (per-site detailed traffic)"
    else:
        mode = "cookie export (HTTP + storage snapshot on stop)"
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
    if args.full_network or args.all_requests:
        capture_mode = CaptureMode.FULL_NETWORK
    else:
        capture_mode = CaptureMode.COOKIE_EXPORT

    task_id = args.task_id or datetime.now().strftime("task_%Y%m%d_%H%M%S")

    return RunConfig(
        task_id=task_id,
        output_dir=resolve_output_dir(args.output_dir),
        chrome=ChromeConfig(host=args.chrome_host, port=args.chrome_port),
        capture_mode=capture_mode,
        include_sensitive=args.include_sensitive,
        flush_interval_s=args.flush_interval,
    )


def prompt_capture_mode() -> CaptureMode:
    log_info("")
    log_info("Select capture mode:")
    log_info("  1 = Full network scan (har site ka detailed traffic)")
    log_info("  2 = Cookie scan only (HTTP + localStorage + IndexedDB export)")
    log_info("")
    while True:
        try:
            choice = input("Enter 1 or 2: ").strip()
        except EOFError:
            return CaptureMode.COOKIE_EXPORT
        if choice == "1":
            return CaptureMode.FULL_NETWORK
        if choice == "2":
            return CaptureMode.COOKIE_EXPORT
        log_info("Invalid choice. Please enter 1 or 2.")


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

    if double_click and not args.full_network and not args.cookie_export and not args.all_requests:
        config = config.model_copy(update={"capture_mode": prompt_capture_mode()})

    print_startup_banner(config)

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
