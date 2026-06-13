"""Runtime configuration."""

from __future__ import annotations

from enum import Enum
from pathlib import Path

from pydantic import BaseModel, Field


class CaptureMode(str, Enum):
    """What to record from the browser."""

    COOKIE_EXPORT = "cookie_export"
    FULL_NETWORK = "full_network"


class ChromeConfig(BaseModel):
    host: str = "127.0.0.1"
    port: int = 9222
    profile_directory: str = "Default"
    user_data_dir: Path | None = None
    use_installed_profile: bool = True
    clone_installed_profile: bool = False
    clone_profile_dir: Path | None = None

    @property
    def cdp_url(self) -> str:
        return f"http://{self.host}:{self.port}"

    def resolved_user_data_dir(self) -> Path | None:
        if self.user_data_dir is not None:
            return self.user_data_dir
        if self.use_installed_profile or self.clone_installed_profile:
            from onf.chrome_profiles import installed_user_data_dir

            return installed_user_data_dir()
        return None


class RunConfig(BaseModel):
    task_id: str
    output_dir: Path = Field(default=Path("./captures"))
    chrome: ChromeConfig = Field(default_factory=ChromeConfig)
    capture_mode: CaptureMode = CaptureMode.COOKIE_EXPORT
    include_sensitive: bool = False
    flush_interval_s: float = 2.0
    auto_launch_chrome: bool = True
    force_restart_chrome: bool = False
    chrome_wait_seconds: float = 25.0

    @property
    def session_dir(self) -> Path:
        return self.output_dir / "sessions" / self.task_id

    @property
    def cookie_export(self) -> bool:
        return self.capture_mode == CaptureMode.COOKIE_EXPORT

    @property
    def full_network(self) -> bool:
        return self.capture_mode == CaptureMode.FULL_NETWORK
