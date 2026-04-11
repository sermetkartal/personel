"""
Risk tier classification based on anomaly score thresholds.

Thresholds:
  normal      : score < 0.30
  watch       : 0.30 <= score < 0.70
  investigate : score >= 0.70

These are read from config so they can be tuned per deployment without
code changes. The defaults above are Phase 2.6 baselines.
"""

from __future__ import annotations

from personel_uba.schemas import RiskTier


def classify_tier(
    score: float,
    watch_threshold: float = 0.3,
    investigate_threshold: float = 0.7,
) -> RiskTier:
    """
    Classify an anomaly score into a risk tier.

    Parameters
    ----------
    score:
        Normalised anomaly score in [0.0, 1.0].
    watch_threshold:
        Score >= this value is classified 'watch'. Default 0.3.
    investigate_threshold:
        Score >= this value is classified 'investigate'. Default 0.7.

    Returns
    -------
    RiskTier
        One of 'normal', 'watch', 'investigate'.

    Notes
    -----
    KVKK m.11/g: tier labels are advisory only. 'investigate' does NOT
    trigger automated disciplinary action or access restriction.
    """
    if score >= investigate_threshold:
        return "investigate"
    if score >= watch_threshold:
        return "watch"
    return "normal"


def tier_to_display(tier: RiskTier) -> str:
    """Return a human-readable Turkish display string for a tier."""
    mapping: dict[RiskTier, str] = {
        "normal": "Normal",
        "watch": "İzlemede",
        "investigate": "İnceleme Gerektirir",
    }
    return mapping[tier]


def tier_ordering(tier: RiskTier) -> int:
    """Return an integer ordering for sorting (investigate=2, watch=1, normal=0)."""
    ordering: dict[RiskTier, int] = {
        "investigate": 2,
        "watch": 1,
        "normal": 0,
    }
    return ordering[tier]
