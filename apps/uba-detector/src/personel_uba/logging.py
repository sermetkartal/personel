"""
Structured logging via structlog.

Every log line at INFO level that mentions a score appends the KVKK disclaimer
to prevent misinterpretation in incident response workflows.

KVKK m.11/g: scores are advisory; the disclaimer is appended automatically.
"""

from __future__ import annotations

import logging
import sys

import structlog

from personel_uba import KVKK_DISCLAIMER


def configure_logging(level: str = "info") -> None:
    """Configure structlog with JSON output suitable for Docker log drivers."""
    log_level = getattr(logging, level.upper(), logging.INFO)

    structlog.configure(
        processors=[
            structlog.contextvars.merge_contextvars,
            structlog.stdlib.add_log_level,
            structlog.stdlib.add_logger_name,
            structlog.processors.TimeStamper(fmt="iso"),
            structlog.processors.StackInfoRenderer(),
            structlog.processors.format_exc_info,
            structlog.processors.UnicodeDecoder(),
            structlog.processors.JSONRenderer(),
        ],
        wrapper_class=structlog.make_filtering_bound_logger(log_level),
        context_class=dict,
        logger_factory=structlog.PrintLoggerFactory(file=sys.stdout),
        cache_logger_on_first_use=True,
    )

    # Also configure stdlib logging so uvicorn/httpx logs are captured
    logging.basicConfig(
        format="%(message)s",
        stream=sys.stdout,
        level=log_level,
    )


def get_logger(name: str) -> structlog.BoundLogger:
    """Return a named structlog logger."""
    return structlog.get_logger(name)


def log_score(
    logger: structlog.BoundLogger,
    user_id: str,
    score: float,
    tier: str,
    **extra: object,
) -> None:
    """
    Log an anomaly score at INFO level with mandatory KVKK disclaimer.

    Every log line that mentions a score MUST include the disclaimer to prevent
    misinterpretation during incident response (KVKK m.11/g requirement).
    """
    logger.info(
        "uba_score_computed",
        user_id=user_id,
        anomaly_score=score,
        risk_tier=tier,
        kvkk_disclaimer=KVKK_DISCLAIMER,
        **extra,
    )
