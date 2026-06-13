"""Launch and verify Chrome remote debugging on Windows."""

from __future__ import annotations

import json
import os
import subprocess
import sys
import tempfile
import time
from pathlib import Path
from urllib.error import URLError
from urllib.request import urlopen

from onf.chrome_profiles import (
    clone_profile_for_debug,
    installed_user_data_dir,
    list_installed_profiles,
    resolve_profile_directory,
)
from onf.config import ChromeConfig
from onf.logging_utils import log_info


def chrome_exe_candidates() -> list[Path]:
    roots = [
        os.environ.get("ProgramFiles", r"C:\Program Files"),
        os.environ.get("ProgramFiles(x86)", r"C:\Program Files (x86)"),
        os.environ.get("LOCALAPPDATA", ""),
    ]
    rel_paths = [
        Path("Google/Chrome/Application/chrome.exe"),
        Path("Google/Chrome Beta/Application/chrome.exe"),
        Path("Chromium/Application/chrome.exe"),
    ]
    candidates: list[Path] = []
    for root in roots:
        if not root:
            continue
        for rel in rel_paths:
            path = Path(root) / rel
            if path.exists():
                candidates.append(path)
    return candidates


def find_chrome_exe() -> Path | None:
    for path in chrome_exe_candidates():
        if path.exists():
            return path
    return None


def profile_root_for_launch(chrome: ChromeConfig) -> tuple[Path, str, str]:
    profile_directory = resolve_profile_directory(
        chrome.profile_directory,
        chrome.resolved_user_data_dir(),
    )

    if chrome.user_data_dir is not None:
        return chrome.user_data_dir, profile_directory, "custom-user-data-dir"

    if chrome.clone_installed_profile:
        installed_root = installed_user_data_dir()
        clone_root = (
            chrome.clone_profile_dir
            if chrome.clone_profile_dir
            else Path(tempfile.gettempdir()) / "onf-profile-clone"
        )
        cloned = clone_profile_for_debug(
            source_user_data_dir=installed_root,
            profile_directory=profile_directory,
            clone_user_data_dir=clone_root,
        )
        return cloned, profile_directory, "cloned-installed-profile"

    if chrome.use_installed_profile:
        return installed_user_data_dir(), profile_directory, "installed-profile"

    legacy = Path(os.environ.get("LOCALAPPDATA", "")) / "onf-chrome-debug"
    return legacy, "Default", "legacy-temp-profile"


def is_debug_port_ready(chrome: ChromeConfig, *, timeout_s: float = 2.0) -> bool:
    version_url = f"{chrome.cdp_url}/json/version"
    try:
        with urlopen(version_url, timeout=timeout_s) as response:
            data = json.loads(response.read().decode("utf-8"))
        return bool(data.get("webSocketDebuggerUrl"))
    except (URLError, OSError, TimeoutError, json.JSONDecodeError, ValueError):
        return False


def is_chrome_running() -> bool:
    if sys.platform != "win32":
        return False
    result = subprocess.run(
        ["tasklist", "/FI", "IMAGENAME eq chrome.exe", "/NH"],
        capture_output=True,
        text=True,
        check=False,
    )
    return "chrome.exe" in result.stdout.lower()


def kill_chrome_processes() -> None:
    if sys.platform != "win32":
        return
    log_info("Saara Chrome band ho raha hai (debug port ke liye restart zaroori hai)...")
    subprocess.run(
        ["taskkill", "/F", "/IM", "chrome.exe"],
        capture_output=True,
        check=False,
    )
    time.sleep(2)


def launch_chrome_debug(chrome: ChromeConfig) -> subprocess.Popen | None:
    chrome_exe = find_chrome_exe()
    if chrome_exe is None:
        raise RuntimeError("Google Chrome install nahi mila. Pehle Chrome install karo.")

    profile_root, profile_directory, launch_mode = profile_root_for_launch(chrome)
    profile_root.mkdir(parents=True, exist_ok=True)

    args = [
        str(chrome_exe),
        f"--remote-debugging-port={chrome.port}",
        "--remote-allow-origins=*",
        f'--user-data-dir={profile_root}',
        f'--profile-directory={profile_directory}',
        "--no-first-run",
        "--no-default-browser-check",
        "--new-window",
        "about:blank",
    ]
    log_info(f"Chrome start ho raha hai ({launch_mode})")
    log_info(f"User data dir: {profile_root}")
    log_info(f"Profile: {profile_directory}")

    creationflags = 0
    if sys.platform == "win32":
        creationflags = subprocess.DETACHED_PROCESS | subprocess.CREATE_NEW_PROCESS_GROUP

    return subprocess.Popen(
        args,
        stdout=subprocess.DEVNULL,
        stderr=subprocess.DEVNULL,
        creationflags=creationflags,
    )


def wait_for_debug_port(chrome: ChromeConfig, *, timeout_s: float) -> bool:
    deadline = time.time() + timeout_s
    while time.time() < deadline:
        if is_debug_port_ready(chrome):
            return True
        time.sleep(1.0)
    return False


def ensure_chrome_debug(
    chrome: ChromeConfig,
    *,
    auto_launch: bool = True,
    initial_wait_s: float = 8.0,
    launch_wait_s: float = 90.0,
) -> None:
    """Wait for debug port; optionally restart Chrome with the chosen real profile."""
    if is_debug_port_ready(chrome):
        return

    if is_chrome_running():
        log_info(
            "Chrome khula hai lekin debug port 9222 par nahi hai.\n"
            "Real profile ke saath restart hoga — extensions/login same profile se aayenge."
        )
    else:
        log_info("Port 9222 par Chrome debug abhi nahi chal raha.")

    deadline = time.time() + initial_wait_s
    while time.time() < deadline:
        if is_debug_port_ready(chrome):
            log_info("Chrome debug port ready.")
            return
        time.sleep(2)

    if not auto_launch:
        raise RuntimeError("Chrome debug port not ready.")

    kill_chrome_processes()
    proc = launch_chrome_debug(chrome)
    if wait_for_debug_port(chrome, timeout_s=min(launch_wait_s, 25.0)):
        log_info("Chrome debug port ready.")
        return

    clone_allowed = (
        chrome.use_installed_profile
        and not chrome.clone_installed_profile
        and chrome.user_data_dir is None
    )
    if clone_allowed and proc is not None:
        try:
            proc.terminate()
        except Exception:
            pass
        kill_chrome_processes()
        log_info("Direct profile launch slow/failed — cloned profile fallback try ho raha hai...")
        cloned_chrome = chrome.model_copy(update={"clone_installed_profile": True})
        launch_chrome_debug(cloned_chrome)
        if wait_for_debug_port(chrome, timeout_s=launch_wait_s):
            log_info("Chrome debug port ready (clone fallback).")
            return

    deadline = time.time() + launch_wait_s
    attempt = 0
    while time.time() < deadline:
        attempt += 1
        if is_debug_port_ready(chrome):
            log_info("Chrome debug port ready.")
            return
        if attempt == 1:
            log_info("Chrome window check karo — profile load ho raha hai...")
        elif attempt % 5 == 0:
            remaining = max(0, int(deadline - time.time()))
            log_info(f"Abhi bhi wait... ({remaining}s baaki)")
        time.sleep(2)

    raise RuntimeError(
        "Chrome debug port start nahi hua. Browser mein test karo: "
        f"{chrome.cdp_url}/json/version"
    )


def format_profile_menu() -> str:
    profiles = list_installed_profiles()
    if not profiles:
        return "Default"
    lines = ["Installed Chrome profiles:"]
    for index, item in enumerate(profiles, start=1):
        lines.append(f'  {index} = {item["name"]}  ({item["directory"]})')
    return "\n".join(lines)


def profile_directory_from_menu_choice(choice: str) -> str:
    profiles = list_installed_profiles()
    if not profiles:
        return "Default"

    text = choice.strip()
    if not text:
        return profiles[0]["directory"]

    if text.isdigit():
        idx = int(text) - 1
        if 0 <= idx < len(profiles):
            return profiles[idx]["directory"]

    return resolve_profile_directory(text)
