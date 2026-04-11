"""
Configuration — pydantic-settings with PERSONEL_ML_ prefix.

All tuneable parameters are environment-variable-driven; no hardcoded secrets.
"""

from __future__ import annotations

import os
from functools import lru_cache

from pydantic import Field, field_validator
from pydantic_settings import BaseSettings, SettingsConfigDict


class Settings(BaseSettings):
    """Runtime configuration for personel-ml.

    All env vars use the PERSONEL_ML_ prefix, e.g.:
      PERSONEL_ML_BIND_HOST=0.0.0.0
      PERSONEL_ML_MODEL_PATH=/models/llama-3.2-3b.Q4_K_M.gguf
    """

    model_config = SettingsConfigDict(
        env_prefix="PERSONEL_ML_",
        env_file=".env",
        env_file_encoding="utf-8",
        case_sensitive=False,
    )

    # --- HTTP server ---
    bind_host: str = Field(default="0.0.0.0", description="Bind address")
    bind_port: int = Field(default=8080, ge=1, le=65535, description="Bind port")

    # --- Model ---
    model_path: str = Field(
        default="/models/llama-3.2-3b.Q4_K_M.gguf",
        description="Absolute path to the GGUF model file",
    )
    model_version: str = Field(
        default="llama-3.2-3b-q4_k_m",
        description="Human-readable model version tag surfaced in API responses",
    )

    # --- llama.cpp inference parameters ---
    n_threads: int = Field(
        default=0,
        ge=0,
        description="CPU thread count for llama.cpp. 0 = auto-detect (half physical cores).",
    )
    n_ctx: int = Field(
        default=2048,
        ge=512,
        le=8192,
        description="Context window size in tokens",
    )
    n_batch: int = Field(
        default=512,
        ge=64,
        le=2048,
        description="Prompt processing batch size",
    )
    n_gpu_layers: int = Field(
        default=0,
        ge=0,
        description="GPU layers to offload. 0 = CPU only (Phase 2 default).",
    )
    max_tokens: int = Field(
        default=64,
        ge=16,
        le=256,
        description="Maximum tokens to generate per classify call",
    )
    temperature: float = Field(
        default=0.0,
        ge=0.0,
        le=2.0,
        description="Sampling temperature. 0 = greedy/deterministic.",
    )

    # --- Classification thresholds ---
    confidence_threshold: float = Field(
        default=0.70,
        ge=0.0,
        le=1.0,
        description="Below this confidence, category is replaced with 'unknown'. ADR 0017.",
    )

    # --- Batch endpoint ---
    batch_max_items: int = Field(
        default=32,
        ge=1,
        le=128,
        description="Maximum items accepted in a single /v1/classify/batch call",
    )

    # --- Observability ---
    log_level: str = Field(
        default="info",
        description="Log level: debug | info | warning | error",
    )
    metrics_enabled: bool = Field(default=True)

    @field_validator("n_threads")
    @classmethod
    def resolve_auto_threads(cls, v: int) -> int:
        if v == 0:
            try:
                cpu_count = os.cpu_count() or 2
                return max(1, cpu_count // 2)
            except Exception:
                return 4
        return v

    @field_validator("log_level")
    @classmethod
    def normalise_log_level(cls, v: str) -> str:
        allowed = {"debug", "info", "warning", "error", "critical"}
        normalised = v.lower()
        if normalised not in allowed:
            raise ValueError(f"log_level must be one of {allowed}")
        return normalised


@lru_cache(maxsize=1)
def get_settings() -> Settings:
    """Return the singleton Settings instance (cached after first call)."""
    return Settings()
