"""
Feature extraction from ClickHouse materialized views.

Operates on pandas DataFrames so that unit tests can inject synthetic data
without requiring a live ClickHouse connection.

Phase 2.6 implements 3 features with full pandas logic:
  - off_hours_activity  (pure pandas, fully tested)
  - app_diversity       (pure pandas, fully tested)
  - data_egress_volume  (pure pandas, fully tested)

Phase 2.6 implements 4 features with simplified logic (tested):
  - screenshot_rate
  - file_access_rate
  - policy_violation_count
  - new_host_ratio

All feature values are raw (un-normalised). Normalisation (z-score per tenant)
is applied in scoring.py before the model receives the feature vector.
"""

from __future__ import annotations

from datetime import datetime, timedelta, timezone

import numpy as np
import pandas as pd

# ---------------------------------------------------------------------------
# Turkish business hours definition (UTC+3, Mon-Fri 08:00-18:00)
# ---------------------------------------------------------------------------
TZ_ISTANBUL = timezone(timedelta(hours=3))
BUSINESS_HOUR_START = 8   # 08:00 inclusive
BUSINESS_HOUR_END = 18    # 18:00 exclusive
BUSINESS_WEEKDAYS = {0, 1, 2, 3, 4}  # Mon=0 … Fri=4


def _is_business_hour(ts: pd.Timestamp) -> bool:
    """Return True if the timestamp falls within Turkish business hours."""
    local = ts.tz_convert(TZ_ISTANBUL) if ts.tzinfo is not None else ts.tz_localize("UTC").tz_convert(TZ_ISTANBUL)
    return local.weekday() in BUSINESS_WEEKDAYS and BUSINESS_HOUR_START <= local.hour < BUSINESS_HOUR_END


# ---------------------------------------------------------------------------
# Feature 1: off_hours_activity
# ---------------------------------------------------------------------------


def compute_off_hours_activity(events: pd.DataFrame) -> float:
    """
    Ratio of events that occurred outside Turkish business hours.

    Parameters
    ----------
    events:
        DataFrame with at least an 'occurred_at' column (datetime64[ns, UTC]
        or naive UTC). Must contain only events for the target user and window.

    Returns
    -------
    float
        Value in [0.0, 1.0]. 0.0 = all events in business hours;
        1.0 = all events outside business hours.
        Returns 0.0 if events is empty.
    """
    if events.empty:
        return 0.0

    col = events["occurred_at"]
    # Ensure timezone-aware
    if col.dt.tz is None:
        col = col.dt.tz_localize("UTC")
    col_istanbul = col.dt.tz_convert(TZ_ISTANBUL)

    is_off = ~(
        col_istanbul.dt.weekday.isin(list(BUSINESS_WEEKDAYS))
        & col_istanbul.dt.hour.ge(BUSINESS_HOUR_START)
        & col_istanbul.dt.hour.lt(BUSINESS_HOUR_END)
    )
    return float(is_off.mean())


# ---------------------------------------------------------------------------
# Feature 2: app_diversity
# ---------------------------------------------------------------------------


def compute_app_diversity(events: pd.DataFrame) -> float:
    """
    Count of distinct application names used in the scoring window.

    Parameters
    ----------
    events:
        DataFrame with at least a 'app_name' column (string). May also accept
        a 'process_name' column as fallback.

    Returns
    -------
    float
        Number of distinct app names (raw count, not normalised here).
        Returns 0.0 if no app_name or process_name column is present.
    """
    if events.empty:
        return 0.0

    if "app_name" in events.columns:
        col = events["app_name"].dropna()
    elif "process_name" in events.columns:
        col = events["process_name"].dropna()
    else:
        return 0.0

    return float(col.nunique())


# ---------------------------------------------------------------------------
# Feature 3: data_egress_volume
# ---------------------------------------------------------------------------


def compute_data_egress_volume(events: pd.DataFrame) -> float:
    """
    Total bytes of data egress: file writes + clipboard content + network outbound.

    Parameters
    ----------
    events:
        DataFrame that may contain:
          - 'bytes_written'    : bytes from file write events
          - 'clipboard_bytes'  : bytes from clipboard copy events
          - 'bytes_out'        : outbound network bytes from flow events

    Returns
    -------
    float
        Sum of all egress bytes. Returns 0.0 if no relevant columns exist.
    """
    if events.empty:
        return 0.0

    total: float = 0.0
    for col in ("bytes_written", "clipboard_bytes", "bytes_out"):
        if col in events.columns:
            total += float(events[col].fillna(0).clip(lower=0).sum())
    return total


# ---------------------------------------------------------------------------
# Feature 4: screenshot_rate
# ---------------------------------------------------------------------------


def compute_screenshot_rate(events: pd.DataFrame, window_hours: float) -> float:
    """
    Average screenshots per hour in the scoring window.

    Parameters
    ----------
    events:
        DataFrame with an 'event_type' column. Screenshot events have
        event_type == 'screen_capture'.
    window_hours:
        Length of the scoring window in hours (e.g. 168 for 7 days).

    Returns
    -------
    float
        Screenshots per hour. Returns 0.0 if no relevant data.
    """
    if events.empty or window_hours <= 0:
        return 0.0

    if "event_type" not in events.columns:
        return 0.0

    count = int((events["event_type"] == "screen_capture").sum())
    return count / window_hours


# ---------------------------------------------------------------------------
# Feature 5: file_access_rate
# ---------------------------------------------------------------------------


def compute_file_access_rate(events: pd.DataFrame, window_hours: float) -> float:
    """
    File read events per hour averaged over the scoring window.

    Parameters
    ----------
    events:
        DataFrame with an 'event_type' column. File read events have
        event_type in {'file_read', 'file_open'}.
    window_hours:
        Scoring window in hours.

    Returns
    -------
    float
        File reads per hour. Returns 0.0 if no relevant data.
    """
    if events.empty or window_hours <= 0:
        return 0.0

    if "event_type" not in events.columns:
        return 0.0

    file_events = {"file_read", "file_open"}
    count = int(events["event_type"].isin(file_events).sum())
    return count / window_hours


# ---------------------------------------------------------------------------
# Feature 6: policy_violation_count
# ---------------------------------------------------------------------------


def compute_policy_violation_count(events: pd.DataFrame) -> float:
    """
    Total count of policy violation events: app/web blocks + DLP matches.

    Parameters
    ----------
    events:
        DataFrame with an 'event_type' column. Violation events have
        event_type in {'app_blocked', 'web_blocked', 'dlp_match'}.

    Returns
    -------
    float
        Raw violation count. Returns 0.0 if no relevant columns.
    """
    if events.empty:
        return 0.0

    if "event_type" not in events.columns:
        return 0.0

    violation_types = {"app_blocked", "web_blocked", "dlp_match"}
    return float(events["event_type"].isin(violation_types).sum())


# ---------------------------------------------------------------------------
# Feature 7: new_host_ratio
# ---------------------------------------------------------------------------


def compute_new_host_ratio(
    current_events: pd.DataFrame,
    baseline_events: pd.DataFrame,
) -> float:
    """
    Fraction of network hosts in the current window not seen in the prior 30 days.

    Parameters
    ----------
    current_events:
        DataFrame for the current scoring window. Must have a 'remote_host'
        column (string: IP or hostname).
    baseline_events:
        DataFrame for the prior 30-day baseline. Same schema.

    Returns
    -------
    float
        Ratio in [0.0, 1.0]. 0.0 = all hosts seen before; 1.0 = all new.
        Returns 0.0 if current_events is empty or has no remote_host column.
    """
    if current_events.empty or "remote_host" not in current_events.columns:
        return 0.0

    current_hosts = set(current_events["remote_host"].dropna().unique())
    if not current_hosts:
        return 0.0

    if baseline_events.empty or "remote_host" not in baseline_events.columns:
        # No baseline data available — treat all as new
        return 1.0

    baseline_hosts = set(baseline_events["remote_host"].dropna().unique())
    new_hosts = current_hosts - baseline_hosts
    return len(new_hosts) / len(current_hosts)


# ---------------------------------------------------------------------------
# High-level: extract all 7 features for a user
# ---------------------------------------------------------------------------


def extract_features(
    user_events: pd.DataFrame,
    baseline_events: pd.DataFrame | None = None,
    window_days: int = 7,
) -> dict[str, float]:
    """
    Extract all 7 UBA features from user event DataFrames.

    Parameters
    ----------
    user_events:
        Events for the target user in the scoring window. Expected columns:
        occurred_at, event_type, app_name (or process_name), bytes_written,
        clipboard_bytes, bytes_out, remote_host.
    baseline_events:
        Events for the prior 30-day baseline period (for new_host_ratio).
        If None, new_host_ratio defaults to 0.0.
    window_days:
        Scoring window length in days. Used to compute per-hour rates.

    Returns
    -------
    dict[str, float]
        Mapping from feature name to raw (un-normalised) float value.
    """
    window_hours = float(window_days * 24)
    _baseline = baseline_events if baseline_events is not None else pd.DataFrame()

    return {
        "off_hours_activity": compute_off_hours_activity(user_events),
        "app_diversity": compute_app_diversity(user_events),
        "data_egress_volume": compute_data_egress_volume(user_events),
        "screenshot_rate": compute_screenshot_rate(user_events, window_hours),
        "file_access_rate": compute_file_access_rate(user_events, window_hours),
        "policy_violation_count": compute_policy_violation_count(user_events),
        "new_host_ratio": compute_new_host_ratio(user_events, _baseline),
    }


# ---------------------------------------------------------------------------
# Normalisation helpers
# ---------------------------------------------------------------------------


def zscore_normalise(
    feature_matrix: np.ndarray,
    mean: np.ndarray | None = None,
    std: np.ndarray | None = None,
) -> tuple[np.ndarray, np.ndarray, np.ndarray]:
    """
    Z-score normalise a feature matrix (n_users x n_features).

    If mean/std are supplied (e.g. from a pre-fitted scaler), they are used
    directly. Otherwise computed from the matrix.

    Returns (normalised_matrix, mean_vector, std_vector).
    """
    if mean is None:
        mean = feature_matrix.mean(axis=0)
    if std is None:
        std = feature_matrix.std(axis=0)

    # Avoid division by zero for constant features
    safe_std = np.where(std == 0, 1.0, std)
    return (feature_matrix - mean) / safe_std, mean, std


def compute_window_start(window_days: int) -> datetime:
    """Return the UTC datetime that is window_days ago from now."""
    return datetime.now(tz=timezone.utc) - timedelta(days=window_days)
