"""CLI entry point."""

from __future__ import annotations

import argparse
import sys
from datetime import datetime
from pathlib import Path

from onf import __version__
from onf.chrome_launcher import format_profile_menu, profile_directory_from_menu_choice
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
    parser.add_argument(
        "--no-auto-chrome",
        action="store_true",
        help="Do not auto-launch Chrome debug mode when port 9222 is not ready.",
    )
    parser.add_argument(
        "--profile-directory",
        default=None,
        help='Installed Chrome profile folder, e.g. "Default" or "Profile 1".',
    )
    parser.add_argument(
        "--use-installed-profile",
        action=argparse.BooleanOptionalAction,
        default=True,
        help="Use real installed Chrome User Data (default: on).",
    )
    parser.add_argument(
        "--clone-installed-profile",
        action="store_true",
        help="Clone installed profile to a temp folder before launch (fallback mode).",
    )
    parser.add_argument(
        "--user-data-dir",
        default=None,
        help="Custom Chrome user-data-dir path (advanced).",
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
    log_info(f"Chrome profile: {config.chrome.profile_directory}")
    if config.chrome.use_installed_profile and config.chrome.user_data_dir is None:
        log_info(f"User data dir : {config.chrome.resolved_user_data_dir()}")
    log_info(f"Task ID      : {config.task_id}")
    log_info(f"Output folder: {config.session_dir}")
    log_info("-" * 56)
    if config.auto_launch_chrome:
        log_info(
            "Real Chrome profile use hoga — extensions/login wahi profile se aayenge."
        )
    else:
        log_info("Pehle Chrome debug mode mein kholo:")
        log_info("  scripts\\start_chrome_debug.bat")
    log_info("Capture ke liye Ctrl+C se band karo.")
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
    profile_directory = args.profile_directory or "Default"
    user_data_dir = Path(args.user_data_dir) if args.user_data_dir else None

    return RunConfig(
        task_id=task_id,
        output_dir=resolve_output_dir(args.output_dir),
        chrome=ChromeConfig(
            host=args.chrome_host,
            port=args.chrome_port,
            profile_directory=profile_directory,
            user_data_dir=user_data_dir,
            use_installed_profile=args.use_installed_profile,
            clone_installed_profile=args.clone_installed_profile,
        ),
        capture_mode=capture_mode,
        include_sensitive=args.include_sensitive,
        flush_interval_s=args.flush_interval,
        auto_launch_chrome=not args.no_auto_chrome,
    )


def prompt_chrome_profile() -> str:
    log_info("")
    log_info(format_profile_menu())
    log_info('Enter number or profile folder name (Enter = first profile):')
    try:
        choice = input("Chrome profile: ").strip()
    except EOFError:
        return "Default"
    try:
        return profile_directory_from_menu_choice(choice)
    except RuntimeError as exc:
        log_info(str(exc))
        return "Default"


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

    if args.profile_directory is None and is_frozen():
        profile_directory = prompt_chrome_profile()
        config = config.model_copy(
            update={
                "chrome": config.chrome.model_copy(
                    update={"profile_directory": profile_directory}
                )
            }
        )

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
