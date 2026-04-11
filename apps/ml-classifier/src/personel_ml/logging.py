"""
Structlog JSON logger with request-id middleware.

All log output is newline-delimited JSON; suitable for log aggregators
(OpenSearch, Loki, etc.) that expect structured input.

Usage:
    from personel_ml.logging import get_logger
    logger = get_logger(__name__)
    logger.info("classify.done", category="work", confidence=0.92)
"""

from __future__ import annotations

import logging
import sys
import uuid
from collections.abc import Awaitable, Callable
from contextvars import ContextVar
from typing import Any

import structlog
from starlette.middleware.base import BaseHTTPMiddleware
from starlette.requests import Request
from starlette.responses import Response

# Context variable so the request_id is available inside handlers without
# being passed around explicitly.
_request_id_var: ContextVar[str] = ContextVar("request_id", default="")


def get_request_id() -> str:
    return _request_id_var.get()


def configure_logging(log_level: str = "info") -> None:
    """Configure structlog for JSON output.  Call once at startup."""

    level = getattr(logging, log_level.upper(), logging.INFO)
    logging.basicConfig(
        format="%(message)s",
        stream=sys.stdout,
        level=level,
    )

    shared_processors: list[Any] = [
        structlog.contextvars.merge_contextvars,
        structlog.stdlib.add_log_level,
        structlog.stdlib.add_logger_name,
        structlog.processors.TimeStamper(fmt="iso", utc=True),
        structlog.processors.StackInfoRenderer(),
        structlog.processors.format_exc_info,
    ]

    structlog.configure(
        processors=[
            *shared_processors,
            structlog.processors.JSONRenderer(),
        ],
        wrapper_class=structlog.make_filtering_bound_logger(level),
        logger_factory=structlog.PrintLoggerFactory(sys.stdout),
        cache_logger_on_first_use=True,
    )


def get_logger(name: str | None = None) -> structlog.BoundLogger:
    return structlog.get_logger(name)


# ---------------------------------------------------------------------------
# Request-ID middleware
# ---------------------------------------------------------------------------


class RequestIdMiddleware(BaseHTTPMiddleware):
    """Inject X-Request-Id into every request.

    - If the caller sends X-Request-Id, honour it (useful for correlation
      with the enricher's own request-id).
    - Otherwise generate a new UUIDv4.
    - Bind the request_id to the structlog context so all log lines within
      the request automatically carry it.
    - Echo the request_id back in the response header.
    """

    async def dispatch(
        self,
        request: Request,
        call_next: Callable[[Request], Awaitable[Response]],
    ) -> Response:
        request_id = request.headers.get("x-request-id") or str(uuid.uuid4())
        token = _request_id_var.set(request_id)

        structlog.contextvars.clear_contextvars()
        structlog.contextvars.bind_contextvars(request_id=request_id)

        try:
            response = await call_next(request)
        finally:
            _request_id_var.reset(token)
            structlog.contextvars.clear_contextvars()

        response.headers["X-Request-Id"] = request_id
        return response
