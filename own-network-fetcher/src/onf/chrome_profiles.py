"""Discover installed Chrome profiles on Windows."""

from __future__ import annotations

import json
import os
import shutil
from pathlib import Path


def installed_user_data_dir() -> Path:
    return Path(os.environ.get("LOCALAPPDATA", "")) / "Google" / "Chrome" / "User Data"


def list_installed_profiles(user_data_dir: Path | None = None) -> list[dict[str, str]]:
    root = user_data_dir or installed_user_data_dir()
    if not root.exists():
        return []

    profiles: dict[str, dict[str, str]] = {}
    local_state = root / "Local State"
    if local_state.exists():
        try:
            data = json.loads(local_state.read_text(encoding="utf-8"))
            cache = data.get("profile", {}).get("info_cache", {})
            for directory, info in cache.items():
                if not isinstance(info, dict):
                    continue
                profiles[directory] = {
                    "directory": directory,
                    "name": str(info.get("name") or directory),
                }
        except (json.JSONDecodeError, OSError):
            pass

    for entry in root.iterdir():
        if not entry.is_dir():
            continue
        if entry.name == "Default" or entry.name.startswith("Profile "):
            profiles.setdefault(
                entry.name,
                {"directory": entry.name, "name": entry.name},
            )

    def sort_key(item: dict[str, str]) -> tuple[int, str]:
        directory = item["directory"]
        if directory == "Default":
            return (0, directory)
        return (1, directory)

    return sorted(profiles.values(), key=sort_key)


def profile_display_name(profile_directory: str, user_data_dir: Path | None = None) -> str:
    for item in list_installed_profiles(user_data_dir):
        if item["directory"] == profile_directory:
            return item["name"]
    return profile_directory


def resolve_profile_directory(name: str | None, user_data_dir: Path | None = None) -> str:
    chosen = (name or "Default").strip() or "Default"
    root = user_data_dir or installed_user_data_dir()
    profile_path = root / chosen
    if profile_path.exists():
        return chosen

    for item in list_installed_profiles(root):
        if item["directory"].lower() == chosen.lower():
            return item["directory"]
        if item["name"].lower() == chosen.lower():
            return item["directory"]

    raise RuntimeError(
        f'Chrome profile "{chosen}" not found under {root}. '
        'Use --profile-directory with a folder like "Default" or "Profile 1".'
    )


def clone_profile_for_debug(
    *,
    source_user_data_dir: Path,
    profile_directory: str,
    clone_user_data_dir: Path,
) -> Path:
    source_profile_dir = source_user_data_dir / profile_directory
    if not source_profile_dir.exists():
        raise RuntimeError(f"Profile folder not found: {source_profile_dir}")

    clone_user_data_dir.mkdir(parents=True, exist_ok=True)
    target_profile_dir = clone_user_data_dir / profile_directory
    if target_profile_dir.exists():
        shutil.rmtree(target_profile_dir, ignore_errors=True)

    ignore_names = shutil.ignore_patterns(
        "Cache",
        "Code Cache",
        "GPUCache",
        "Service Worker",
        "Crashpad",
        "GrShaderCache",
        "ShaderCache",
        "DawnCache",
        "VideoDecodeStats",
    )
    shutil.copytree(source_profile_dir, target_profile_dir, ignore=ignore_names)

    local_state_src = source_user_data_dir / "Local State"
    local_state_dst = clone_user_data_dir / "Local State"
    if local_state_src.exists():
        shutil.copy2(local_state_src, local_state_dst)

    return clone_user_data_dir
