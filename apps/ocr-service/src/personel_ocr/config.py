"""
Configuration — pydantic-settings with PERSONEL_OCR_ prefix.

All tuneable parameters are environment-variable-driven; no hardcoded secrets.
"""

from __future__ import annotations

from functools import lru_cache
from typing import Literal

from pydantic import Field, field_validator
from pydantic_settings import BaseSettings, SettingsConfigDict


class Settings(BaseSettings):
    """Runtime configuration for personel-ocr.

    All env vars use the PERSONEL_OCR_ prefix, e.g.:
      PERSONEL_OCR_BIND_HOST=0.0.0.0
      PERSONEL_OCR_DEFAULT_ENGINE=tesseract
      PERSONEL_OCR_DEFAULT_LANGUAGES=tr,en
    """

    model_config = SettingsConfigDict(
        env_prefix="PERSONEL_OCR_",
        env_file=".env",
        env_file_encoding="utf-8",
        case_sensitive=False,
    )

    # --- HTTP server ---
    bind_host: str = Field(default="0.0.0.0", description="Bind address")
    bind_port: int = Field(default=8080, ge=1, le=65535, description="Bind port")

    # --- Engine defaults ---
    default_engine: Literal["tesseract", "paddle", "auto"] = Field(
        default="tesseract",
        description="Default OCR engine when caller does not specify engine_hint.",
    )
    default_languages: list[str] = Field(
        default=["tr", "en"],
        description="Default language codes passed to the OCR engine.",
    )

    # --- Tesseract ---
    tesseract_cmd: str = Field(
        default="tesseract",
        description="Path to the tesseract binary. Overrides pytesseract.pytesseract.tesseract_cmd.",
    )
    tesseract_config: str = Field(
        default="--oem 3 --psm 3",
        description="Extra Tesseract config string passed via pytesseract.",
    )

    # --- PaddleOCR ---
    paddle_use_gpu: bool = Field(
        default=False,
        description="Whether to enable GPU acceleration for PaddleOCR. Default off for Phase 2.",
    )
    paddle_lang: str = Field(
        default="en",
        description=(
            "PaddleOCR language model to load. 'en' covers Latin script; "
            "use 'ch' for CJK fallback. Turkish uses Tesseract; Paddle is supplementary."
        ),
    )

    # --- Preprocessing ---
    preprocess_grayscale: bool = Field(default=True, description="Convert image to grayscale.")
    preprocess_autocontrast: bool = Field(default=True, description="Apply PIL auto-contrast.")
    preprocess_threshold: bool = Field(
        default=False,
        description=(
            "Apply Otsu-like binary threshold. Off by default — can hurt quality "
            "on anti-aliased screenshots. Enable for scanned documents."
        ),
    )

    # --- Postprocessing ---
    confidence_threshold: float = Field(
        default=0.30,
        ge=0.0,
        le=1.0,
        description="Words below this confidence are dropped from final text.",
    )

    # --- Batch ---
    batch_max_items: int = Field(
        default=16,
        ge=1,
        le=64,
        description="Maximum items accepted in a single /v1/extract/batch call.",
    )

    # --- Observability ---
    log_level: str = Field(
        default="info",
        description="Log level: debug | info | warning | error",
    )
    metrics_enabled: bool = Field(default=True)

    @field_validator("log_level")
    @classmethod
    def normalise_log_level(cls, v: str) -> str:
        allowed = {"debug", "info", "warning", "error", "critical"}
        normalised = v.lower()
        if normalised not in allowed:
            raise ValueError(f"log_level must be one of {allowed}")
        return normalised

    @field_validator("default_languages", mode="before")
    @classmethod
    def parse_languages(cls, v: object) -> list[str]:
        """Accept comma-separated string from env var or a list."""
        if isinstance(v, str):
            return [lang.strip() for lang in v.split(",") if lang.strip()]
        return list(v)  # type: ignore[arg-type]


@lru_cache(maxsize=1)
def get_settings() -> Settings:
    """Return the singleton Settings instance (cached after first call)."""
    return Settings()
