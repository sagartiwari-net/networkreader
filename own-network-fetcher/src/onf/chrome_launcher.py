"""Launch and verify Chrome remote debugging on Windows."""

from __future__ import annotations

import json
import os
import subprocess
import sys
import time
from pathlib import Path
from urllib.error import URLError
from urllib.request import urlopen

from onf.chrome_profiles import (
    clone_profile_for_debug,
    installed_user_data_dir,
    list_installed_profiles,
    onf_debug_user_data_dir,
    profile_display_name,
    resolve_profile_directory,
    sync_profile_for_debug,
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
            else onf_debug_user_data_dir()
        )
        cloned = clone_profile_for_debug(
            source_user_data_dir=installed_root,
            profile_directory=profile_directory,
            clone_user_data_dir=clone_root,
        )
        return cloned, profile_directory, "cloned-installed-profile"

    if chrome.use_installed_profile:
        installed_root = installed_user_data_dir()
        debug_root = onf_debug_user_data_dir()
        log_info(
            "Profile sync ho raha hai ONF debug folder mein "
            f"({profile_display_name(profile_directory)} — Chrome 136+ ke liye zaroori)..."
        )
        sync_profile_for_debug(
            source_user_data_dir=installed_root,
            profile_directory=profile_directory,
            debug_user_data_dir=debug_root,
        )
        log_info(f"Profile sync complete: {debug_root}")
        return debug_root, profile_directory, "onf-debug-profile"

    legacy = onf_debug_user_data_dir()
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


def clear_chrome_singleton_artifacts(user_data_dir: Path) -> None:
    for name in ("SingletonLock", "SingletonCookie", "SingletonSocket"):
        path = user_data_dir / name
        try:
            if path.exists() or path.is_symlink():
                path.unlink()
        except OSError:
            pass


def kill_chrome_processes() -> None:
    if sys.platform != "win32":
        return
    log_info("Chrome band ho raha hai...")
    subprocess.run(
        ["taskkill", "/F", "/T", "/IM", "chrome.exe"],
        capture_output=True,
        check=False,
    )
    time.sleep(1.0)


def wait_until_chrome_stopped(*, timeout_s: float = 20.0) -> bool:
    if sys.platform != "win32":
        return True
    deadline = time.time() + timeout_s
    while time.time() < deadline:
        if not is_chrome_running():
            time.sleep(0.8)
            if not is_chrome_running():
                return True
        time.sleep(0.5)
    kill_chrome_processes()
    time.sleep(2.0)
    return not is_chrome_running()


def launch_chrome_debug(chrome: ChromeConfig) -> subprocess.Popen | None:
    chrome_exe = find_chrome_exe()
    if chrome_exe is None:
        raise RuntimeError("Google Chrome install nahi mila. Pehle Chrome install karo.")

    profile_root, profile_directory, launch_mode = profile_root_for_launch(chrome)
    profile_root.mkdir(parents=True, exist_ok=True)
    clear_chrome_singleton_artifacts(profile_root)

    args = [
        str(chrome_exe),
        f"--remote-debugging-port={chrome.port}",
        "--remote-debugging-address=127.0.0.1",
        "--remote-allow-origins=*",
        f"--user-data-dir={profile_root}",
        f"--profile-directory={profile_directory}",
        "--no-first-run",
        "--no-default-browser-check",
        "--new-window",
        "about:blank",
    ]
    log_info(f"Chrome start ({launch_mode}): {profile_display_name(profile_directory)}")
    log_info(f"User data dir: {profile_root}")
    log_info(f"Profile folder: {profile_directory}")

    return subprocess.Popen(
        args,
        stdout=subprocess.DEVNULL,
        stderr=subprocess.DEVNULL,
        close_fds=False,
    )


def wait_for_debug_port(chrome: ChromeConfig, *, timeout_s: float) -> bool:
    deadline = time.time() + timeout_s
    while time.time() < deadline:
        if is_debug_port_ready(chrome, timeout_s=1.0):
            return True
        time.sleep(0.5)
    return False


def ensure_chrome_debug(
    chrome: ChromeConfig,
    *,
    auto_launch: bool = True,
    force_restart: bool = False,
    launch_wait_s: float = 60.0,
) -> None:
    """Connect to debug port; launch only when Chrome is not already debug-ready."""
    if is_debug_port_ready(chrome):
        log_info("Debug port ready — turant connect ho raha hai (Chrome band nahi hoga).")
        return

    if is_chrome_running():
        if force_restart:
            log_info("Force restart ON — Chrome band karke dubara khulega.")
            kill_chrome_processes()
            if not wait_until_chrome_stopped():
                raise RuntimeError(
                    "Chrome poori tarah band nahi hua (Task Manager se saare chrome.exe band karo, phir dubara try karo)."
                )
        elif auto_launch:
            raise RuntimeError(
                "Chrome pehle se chal raha hai lekin debug port 9222 par nahi.\n"
                "Mode 1 (full network) aur Mode 2 (cookies) DONO ke liye debug port zaroori hai.\n"
                "ONF aapka Chrome automatically band NAHI karega.\n\n"
                "Option A (recommended, fast):\n"
                "  ONF dubara chalao aur Y dabao — profile sync + debug Chrome khulega\n\n"
                "Option B:\n"
                "  scripts\\launch_chrome_profile.bat chalao, phir Start ONF.bat"
            )
        else:
            raise RuntimeError(
                "Debug port 9222 ready nahi. Pehle scripts\\launch_chrome_profile.bat chalao."
            )
    elif not auto_launch:
        raise RuntimeError("Debug port ready nahi. scripts\\launch_chrome_profile.bat chalao.")

    clear_chrome_singleton_artifacts(onf_debug_user_data_dir())

    attempts = 2
    per_attempt_wait = max(launch_wait_s / attempts, 20.0)
    for attempt in range(1, attempts + 1):
        if attempt > 1:
            log_info(f"Debug port retry {attempt}/{attempts}...")
            kill_chrome_processes()
            wait_until_chrome_stopped()
            clear_chrome_singleton_artifacts(onf_debug_user_data_dir())

        if not is_debug_port_ready(chrome):
            launch_chrome_debug(chrome)

        if wait_for_debug_port(chrome, timeout_s=per_attempt_wait):
            log_info("Chrome debug port ready.")
            return

    if is_chrome_running() and not is_debug_port_ready(chrome):
        raise RuntimeError(
            "Chrome khula hai lekin debug port 9222 par nahi.\n"
            "Chrome 136+ real profile folder par debug allow nahi karta — ONF ab onf-chrome-debug folder use karta hai.\n"
            "Task Manager se saara Chrome band karo, phir ONF dubara chalao aur Y dabao.\n"
            f"Test URL: {chrome.cdp_url}/json/version"
        )

    if chrome.clone_installed_profile:
        raise RuntimeError("Clone profile ke saath bhi debug port start nahi hua.")

    raise RuntimeError(
        "Debug port start nahi hua. Browser mein test karo: "
        f"{chrome.cdp_url}/json/version"
    )


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
        raise RuntimeError(f"Galat number. List mein se 1 se {len(profiles)} enter karo.")

    lowered = text.lower()
    for item in profiles:
        if item["directory"].lower() == lowered or item["name"].lower() == lowered:
            return item["directory"]

    partial = [
        item
        for item in profiles
        if lowered in item["name"].lower() or lowered in item["directory"].lower()
    ]
    if len(partial) == 1:
        return partial[0]["directory"]

    options = ", ".join(str(index) for index in range(1, len(profiles) + 1))
    raise RuntimeError(f'"{text}" match nahi hua. Sirf number enter karo: {options}')


def format_profile_menu() -> str:
    profiles = list_installed_profiles()
    if not profiles:
        return "Koi installed Chrome profile nahi mila — Default use hoga."
    lines = ["Chrome profile select karo (sirf number enter karo):"]
    for index, item in enumerate(profiles, start=1):
        lines.append(f'  {index} = {item["name"]}')
    return "\n".join(lines)
