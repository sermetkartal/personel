"""
ML model wrappers for UBA anomaly detection.

Phase 2.6: IsolationForestDetector — sklearn IsolationForest wrapper.
Phase 2.7 (deferred): LSTMDetector — temporal sequence anomaly detection.

The IsolationForestDetector.score() method returns a float in [0.0, 1.0]
where 1.0 is the most anomalous. This is the inverse of sklearn's
decision_function (which returns negative for anomalies).

Note on serialisation: joblib is used for model persistence (sklearn-native
format, safer than raw pickle for sklearn estimators). Model files MUST only
be loaded from the trusted local model_dir; do not load from untrusted sources.
"""

from __future__ import annotations

import time
from dataclasses import dataclass, field
from pathlib import Path

import joblib
import numpy as np
from sklearn.ensemble import IsolationForest
from sklearn.preprocessing import StandardScaler

from personel_uba.metrics import MODEL_FIT_DURATION, MODEL_LAST_FIT_TIMESTAMP


# ---------------------------------------------------------------------------
# IsolationForestDetector
# ---------------------------------------------------------------------------


@dataclass
class IsolationForestDetector:
    """
    sklearn IsolationForest wrapper producing normalised [0, 1] anomaly scores.

    Usage
    -----
    detector = IsolationForestDetector(tenant_id="abc", n_estimators=100)
    detector.fit(feature_matrix)          # shape (n_users, 7)
    score = detector.score(user_vector)   # shape (7,) -> float in [0, 1]
    """

    tenant_id: str = field(default="default")
    n_estimators: int = field(default=100)
    contamination: float = field(default=0.05)
    random_state: int = field(default=42)

    _model: IsolationForest | None = field(default=None, init=False, repr=False)
    _scaler: StandardScaler | None = field(default=None, init=False, repr=False)
    _is_fitted: bool = field(default=False, init=False, repr=False)
    _fit_timestamp: float = field(default=0.0, init=False, repr=False)
    _n_features: int = field(default=7, init=False, repr=False)

    def fit(self, feature_matrix: np.ndarray) -> None:
        """
        Fit the IsolationForest on a (n_users x n_features) feature matrix.

        The matrix is z-score scaled before fitting. The scaler is saved so
        that score() can apply the same transformation.

        Parameters
        ----------
        feature_matrix:
            numpy array of shape (n_users, 7). Must have at least 2 rows.
        """
        if feature_matrix.ndim != 2 or feature_matrix.shape[1] != 7:  # noqa: PLR2004
            msg = f"feature_matrix must have shape (n_users, 7), got {feature_matrix.shape}"
            raise ValueError(msg)

        if feature_matrix.shape[0] < 2:  # noqa: PLR2004
            msg = "Need at least 2 user samples to fit IsolationForest"
            raise ValueError(msg)

        with MODEL_FIT_DURATION.time():
            self._scaler = StandardScaler()
            scaled = self._scaler.fit_transform(feature_matrix)

            self._model = IsolationForest(
                n_estimators=self.n_estimators,
                contamination=self.contamination,
                random_state=self.random_state,
                n_jobs=-1,
            )
            self._model.fit(scaled)

        self._is_fitted = True
        self._fit_timestamp = time.time()
        MODEL_LAST_FIT_TIMESTAMP.labels(tenant_id=self.tenant_id).set(self._fit_timestamp)

    def score(self, feature_vector: np.ndarray) -> float:
        """
        Compute anomaly score for a single user feature vector.

        Returns a float in [0.0, 1.0] where:
          - 0.0 = highly normal (deep inside the normal region)
          - 1.0 = highly anomalous

        The raw sklearn decision_function output is in (-inf, 0.5].
        Negative values indicate anomalies. We map this to [0, 1] by:
          normalised = clip(0.5 - raw_decision_value, 0, 1)

        Parameters
        ----------
        feature_vector:
            1D numpy array of shape (7,) with raw (un-normalised) feature values.

        Returns
        -------
        float in [0.0, 1.0]
        """
        if not self._is_fitted or self._model is None or self._scaler is None:
            msg = "Model has not been fitted yet. Call fit() first."
            raise RuntimeError(msg)

        if feature_vector.ndim == 1:
            feature_vector = feature_vector.reshape(1, -1)

        if feature_vector.shape[1] != 7:  # noqa: PLR2004
            msg = f"feature_vector must have 7 features, got {feature_vector.shape[1]}"
            raise ValueError(msg)

        scaled = self._scaler.transform(feature_vector)
        # decision_function: positive = normal, more negative = more anomalous
        raw = self._model.decision_function(scaled)[0]

        # Map to [0, 1]: invert and normalise
        score = float(np.clip(0.5 - raw, 0.0, 1.0))
        return score

    def score_batch(self, feature_matrix: np.ndarray) -> np.ndarray:
        """
        Compute anomaly scores for a batch of users.

        Parameters
        ----------
        feature_matrix:
            2D numpy array of shape (n_users, 7).

        Returns
        -------
        np.ndarray of shape (n_users,) with scores in [0.0, 1.0].
        """
        if not self._is_fitted or self._model is None or self._scaler is None:
            msg = "Model has not been fitted yet. Call fit() first."
            raise RuntimeError(msg)

        scaled = self._scaler.transform(feature_matrix)
        raw = self._model.decision_function(scaled)
        return np.clip(0.5 - raw, 0.0, 1.0)

    @property
    def is_fitted(self) -> bool:
        return self._is_fitted

    @property
    def fit_timestamp(self) -> float:
        return self._fit_timestamp

    def save(self, model_dir: str) -> None:
        """Persist model and scaler to disk using joblib (sklearn-native format)."""
        if not self._is_fitted:
            msg = "Cannot save unfitted model"
            raise RuntimeError(msg)

        path = Path(model_dir) / f"isof_{self.tenant_id}.joblib"
        joblib.dump(
            {
                "model": self._model,
                "scaler": self._scaler,
                "fit_timestamp": self._fit_timestamp,
                "tenant_id": self.tenant_id,
            },
            str(path),
        )

    @classmethod
    def load(cls, model_dir: str, tenant_id: str) -> "IsolationForestDetector":
        """
        Load a persisted model from disk.

        SECURITY: Only load from trusted local model_dir. Do not load models
        from untrusted or user-controlled paths.
        """
        path = Path(model_dir) / f"isof_{tenant_id}.joblib"
        if not path.exists():
            msg = f"No model found at {path}"
            raise FileNotFoundError(msg)

        data = joblib.load(str(path))

        instance = cls(tenant_id=tenant_id)
        instance._model = data["model"]  # noqa: SLF001
        instance._scaler = data["scaler"]  # noqa: SLF001
        instance._fit_timestamp = data["fit_timestamp"]  # noqa: SLF001
        instance._is_fitted = True  # noqa: SLF001
        return instance


# ---------------------------------------------------------------------------
# Phase 2.7 placeholder: LSTM detector
# ---------------------------------------------------------------------------


class LSTMDetector:
    """
    LSTM-based temporal anomaly detector.

    STATUS: DEFERRED to Phase 2.7. This class is a placeholder only.

    In Phase 2.7 this will wrap a PyTorch LSTM trained on per-user event
    time series. The Phase 2.6 scoring pipeline uses IsolationForestDetector.
    """

    def __init__(self) -> None:
        self._fitted = False

    def fit(self, _sequences: object) -> None:
        msg = "LSTMDetector is not implemented in Phase 2.6. Deferred to Phase 2.7."
        raise NotImplementedError(msg)

    def score(self, _sequence: object) -> float:
        msg = "LSTMDetector is not implemented in Phase 2.6. Deferred to Phase 2.7."
        raise NotImplementedError(msg)
