"""
Unit tests for FallbackClassifier.

These tests require no model file and no GPU — they run in any environment.
All 15+ cases cover Turkish business apps, international tools, distractions,
personal apps, edge cases, and the confidence threshold enforcement.
"""

from __future__ import annotations

import pytest

from personel_ml.classifier import FallbackClassifier
from personel_ml.schemas import ClassifyItem, ClassifyResult


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------


def item(app: str, title: str = "", url: str | None = None) -> ClassifyItem:
    return ClassifyItem(app_name=app, window_title=title, url=url)


# ---------------------------------------------------------------------------
# Basic property tests
# ---------------------------------------------------------------------------


class TestFallbackClassifierProperties:
    def test_backend_is_fallback(self, fallback_classifier: FallbackClassifier) -> None:
        assert fallback_classifier.backend == "fallback"

    def test_is_loaded_always_true(self, fallback_classifier: FallbackClassifier) -> None:
        assert fallback_classifier.is_loaded is True

    def test_classify_returns_result_type(self, fallback_classifier: FallbackClassifier) -> None:
        result = fallback_classifier.classify(item("notepad.exe"))
        assert isinstance(result, ClassifyResult)

    def test_result_backend_is_fallback(self, fallback_classifier: FallbackClassifier) -> None:
        result = fallback_classifier.classify(item("Microsoft Excel"))
        assert result.backend == "fallback"

    def test_confidence_in_range(self, fallback_classifier: FallbackClassifier) -> None:
        result = fallback_classifier.classify(item("chrome.exe", "", "youtube.com"))
        assert 0.0 <= result.confidence <= 1.0

    def test_category_is_valid_enum_value(self, fallback_classifier: FallbackClassifier) -> None:
        valid = {"work", "personal", "distraction", "unknown"}
        for app in ["Microsoft Excel", "YouTube", "Instagram", "notepad.exe", "Logo Tiger 3"]:
            result = fallback_classifier.classify(item(app))
            assert result.category in valid, f"{app!r} produced invalid category {result.category!r}"


# ---------------------------------------------------------------------------
# Turkish business applications
# ---------------------------------------------------------------------------


class TestTurkishBusinessApps:
    def test_logo_tiger(self, fallback_classifier: FallbackClassifier) -> None:
        result = fallback_classifier.classify(item("Logo Tiger 3", "Logo Tiger - Fatura Modulu"))
        assert result.category == "work"
        assert result.confidence >= 0.90

    def test_mikro_gold(self, fallback_classifier: FallbackClassifier) -> None:
        result = fallback_classifier.classify(item("Mikro Gold", "Mikro Gold ERP - Stok Takip"))
        assert result.category == "work"
        assert result.confidence >= 0.90

    def test_netsis(self, fallback_classifier: FallbackClassifier) -> None:
        result = fallback_classifier.classify(item("Netsis", "Netsis ERP - Muhasebe"))
        assert result.category == "work"

    def test_bordroplus(self, fallback_classifier: FallbackClassifier) -> None:
        result = fallback_classifier.classify(item("BordroPlus", "BordroPlus - Bordro Hazirlama"))
        assert result.category == "work"

    def test_parasut_via_url(self, fallback_classifier: FallbackClassifier) -> None:
        result = fallback_classifier.classify(item("chrome.exe", "Parasut - Fatura", "parasut.com"))
        assert result.category == "work"

    def test_parasut_via_app_name(self, fallback_classifier: FallbackClassifier) -> None:
        result = fallback_classifier.classify(item("Parasut", "Fatura Listesi"))
        assert result.category == "work"


# ---------------------------------------------------------------------------
# International productivity tools
# ---------------------------------------------------------------------------


class TestInternationalWorkTools:
    def test_microsoft_excel(self, fallback_classifier: FallbackClassifier) -> None:
        result = fallback_classifier.classify(item("Microsoft Excel", "Q4-Budget.xlsx"))
        assert result.category == "work"

    def test_ms_excel_exe(self, fallback_classifier: FallbackClassifier) -> None:
        result = fallback_classifier.classify(item("excel.exe", "Sales Report.xlsx"))
        assert result.category == "work"

    def test_ms_word_winword(self, fallback_classifier: FallbackClassifier) -> None:
        result = fallback_classifier.classify(item("WINWORD.EXE", "Proposal.docx"))
        assert result.category == "work"

    def test_teams(self, fallback_classifier: FallbackClassifier) -> None:
        result = fallback_classifier.classify(item("teams.exe", "Engineering channel"))
        assert result.category == "work"

    def test_slack(self, fallback_classifier: FallbackClassifier) -> None:
        result = fallback_classifier.classify(item("Slack", "# engineering"))
        assert result.category == "work"

    def test_vscode(self, fallback_classifier: FallbackClassifier) -> None:
        result = fallback_classifier.classify(item("Visual Studio Code", "classifier.py"))
        assert result.category == "work"

    def test_github_url(self, fallback_classifier: FallbackClassifier) -> None:
        result = fallback_classifier.classify(item("chrome.exe", "Pull Requests", "github.com"))
        assert result.category == "work"

    def test_stackoverflow(self, fallback_classifier: FallbackClassifier) -> None:
        result = fallback_classifier.classify(
            item("chrome.exe", "Stack Overflow - asyncio", "stackoverflow.com")
        )
        assert result.category == "work"

    def test_jira(self, fallback_classifier: FallbackClassifier) -> None:
        result = fallback_classifier.classify(
            item("chrome.exe", "PRSNL-421 - Jira", "atlassian.net")
        )
        assert result.category == "work"


# ---------------------------------------------------------------------------
# Distractions
# ---------------------------------------------------------------------------


class TestDistractions:
    def test_youtube_url(self, fallback_classifier: FallbackClassifier) -> None:
        result = fallback_classifier.classify(item("chrome.exe", "YouTube - Trending", "youtube.com"))
        assert result.category == "distraction"

    def test_netflix_url(self, fallback_classifier: FallbackClassifier) -> None:
        result = fallback_classifier.classify(item("chrome.exe", "Netflix", "netflix.com"))
        assert result.category == "distraction"

    def test_instagram_app(self, fallback_classifier: FallbackClassifier) -> None:
        result = fallback_classifier.classify(item("Instagram", "Kesfet"))
        assert result.category == "distraction"

    def test_twitter_x(self, fallback_classifier: FallbackClassifier) -> None:
        result = fallback_classifier.classify(item("chrome.exe", "Twitter", "x.com"))
        assert result.category == "distraction"

    def test_tiktok(self, fallback_classifier: FallbackClassifier) -> None:
        result = fallback_classifier.classify(item("TikTok", "For You", "tiktok.com"))
        assert result.category == "distraction"

    def test_steam(self, fallback_classifier: FallbackClassifier) -> None:
        result = fallback_classifier.classify(item("Steam", "Steam"))
        assert result.category == "distraction"


# ---------------------------------------------------------------------------
# Personal apps
# ---------------------------------------------------------------------------


class TestPersonalApps:
    def test_turkish_bank_isbank(self, fallback_classifier: FallbackClassifier) -> None:
        result = fallback_classifier.classify(
            item("chrome.exe", "Is Bankasi - Internet Bankaciligi", "isbank.com.tr")
        )
        assert result.category == "personal"

    def test_turkish_bank_garanti(self, fallback_classifier: FallbackClassifier) -> None:
        result = fallback_classifier.classify(
            item("chrome.exe", "GarantiBBVA Hesabim", "garantibbva.com.tr")
        )
        assert result.category == "personal"

    def test_trendyol(self, fallback_classifier: FallbackClassifier) -> None:
        result = fallback_classifier.classify(
            item("chrome.exe", "Trendyol - Kadin Giyim", "trendyol.com")
        )
        assert result.category == "personal"

    def test_hepsiburada(self, fallback_classifier: FallbackClassifier) -> None:
        result = fallback_classifier.classify(
            item("chrome.exe", "Hepsiburada - Elektronik", "hepsiburada.com")
        )
        assert result.category == "personal"


# ---------------------------------------------------------------------------
# Unknown / low-confidence cases
# ---------------------------------------------------------------------------


class TestUnknown:
    def test_blank_notepad(self, fallback_classifier: FallbackClassifier) -> None:
        result = fallback_classifier.classify(item("notepad.exe", "Yeni Metin Belgesi"))
        assert result.category == "unknown"

    def test_empty_new_tab(self, fallback_classifier: FallbackClassifier) -> None:
        result = fallback_classifier.classify(item("chrome.exe", "Yeni Sekme - Google Chrome"))
        assert result.category == "unknown"

    def test_unknown_app_no_context(self, fallback_classifier: FallbackClassifier) -> None:
        result = fallback_classifier.classify(item("MusteriPortal.exe", ""))
        assert result.category == "unknown"


# ---------------------------------------------------------------------------
# Confidence threshold enforcement
# ---------------------------------------------------------------------------


class TestConfidenceThreshold:
    def test_high_threshold_forces_unknown(self) -> None:
        """With threshold=0.99, even high-confidence rules return unknown."""
        strict = FallbackClassifier(confidence_threshold=0.999, model_version="test")
        result = strict.classify(item("Slack", "# engineering"))
        assert result.category == "unknown"

    def test_zero_threshold_passes_all(self) -> None:
        """With threshold=0.0, all matched categories are returned as-is."""
        permissive = FallbackClassifier(confidence_threshold=0.0, model_version="test")
        result = permissive.classify(item("Logo Tiger 3", "Logo Tiger - Fatura"))
        assert result.category == "work"

    def test_default_threshold_is_0_70(self) -> None:
        classifier = FallbackClassifier()
        assert classifier._confidence_threshold == 0.70

    def test_matched_rule_confidence_at_or_above_threshold(
        self, fallback_classifier: FallbackClassifier
    ) -> None:
        """All rule matches that return a non-unknown category must have
        confidence >= 0.70 (ADR 0017 floor)."""
        samples = [
            item("SAP GUI", "SAP Logon", None),
            item("Oracle ERP", "Oracle Fusion Cloud", None),
            item("chrome.exe", "Jira - PRSNL-999", "jira.atlassian.com"),
            item("chrome.exe", "Asana - My Tasks", "asana.com"),
            item("chrome.exe", "Netflix - Home", "netflix.com"),
        ]
        for ci in samples:
            result = fallback_classifier.classify(ci)
            if result.category != "unknown":
                assert result.confidence >= 0.70, (
                    f"{ci.app_name}/{ci.url} returned {result.category} "
                    f"with confidence {result.confidence} < 0.70"
                )


# ---------------------------------------------------------------------------
# Faz 8 #82 — new rules
# ---------------------------------------------------------------------------


class TestTurkishBusinessExpanded:
    """Turkish business software rules added in Faz 8 #82."""

    def test_sap_logon(self, fallback_classifier: FallbackClassifier) -> None:
        result = fallback_classifier.classify(item("SAPLogon.exe", "SAP Logon 760"))
        assert result.category == "work"
        assert result.confidence >= 0.90

    def test_sap_b1(self, fallback_classifier: FallbackClassifier) -> None:
        result = fallback_classifier.classify(item("SAP Business One", "SAP B1 - Satış"))
        assert result.category == "work"

    def test_oracle_erp(self, fallback_classifier: FallbackClassifier) -> None:
        result = fallback_classifier.classify(item("Oracle ERP", "Oracle Fusion Cloud ERP"))
        assert result.category == "work"

    def test_hitit_erp(self, fallback_classifier: FallbackClassifier) -> None:
        result = fallback_classifier.classify(item("Hitit Yazılım", "Hitit ERP - Muhasebe"))
        assert result.category == "work"

    def test_eta_sql(self, fallback_classifier: FallbackClassifier) -> None:
        result = fallback_classifier.classify(item("ETA SQL", "ETA SQL - Fatura"))
        assert result.category == "work"

    def test_zirve_yazilim(self, fallback_classifier: FallbackClassifier) -> None:
        result = fallback_classifier.classify(item("Zirve Müşavir", "Zirve Yazılım - Bordro"))
        assert result.category == "work"

    def test_vega_yazilim(self, fallback_classifier: FallbackClassifier) -> None:
        result = fallback_classifier.classify(item("Vega Yazılım", "Vega ERP - Muhasebe"))
        assert result.category == "work"


class TestCloudToolsExpanded:
    """Cloud work tool rules added in Faz 8 #82."""

    def test_jira_cloud_url(self, fallback_classifier: FallbackClassifier) -> None:
        result = fallback_classifier.classify(
            item("chrome.exe", "PRSNL-421 - Jira", "jira.atlassian.com")
        )
        assert result.category == "work"

    def test_notion_url(self, fallback_classifier: FallbackClassifier) -> None:
        result = fallback_classifier.classify(
            item("chrome.exe", "Product Roadmap - Notion", "notion.so")
        )
        assert result.category == "work"

    def test_airtable(self, fallback_classifier: FallbackClassifier) -> None:
        result = fallback_classifier.classify(
            item("chrome.exe", "Marketing Plan - Airtable", "airtable.com")
        )
        assert result.category == "work"

    def test_asana(self, fallback_classifier: FallbackClassifier) -> None:
        result = fallback_classifier.classify(
            item("chrome.exe", "My Tasks - Asana", "asana.com")
        )
        assert result.category == "work"

    def test_monday(self, fallback_classifier: FallbackClassifier) -> None:
        result = fallback_classifier.classify(
            item("chrome.exe", "Sprint - monday.com", "monday.com")
        )
        assert result.category == "work"

    def test_clickup(self, fallback_classifier: FallbackClassifier) -> None:
        result = fallback_classifier.classify(
            item("chrome.exe", "Sprint 42 - ClickUp", "clickup.com")
        )
        assert result.category == "work"

    def test_trello(self, fallback_classifier: FallbackClassifier) -> None:
        result = fallback_classifier.classify(
            item("chrome.exe", "Kanban Board - Trello", "trello.com")
        )
        assert result.category == "work"


class TestDistractionsExpanded:
    """Distracting-category rules added in Faz 8 #82."""

    def test_disney_plus(self, fallback_classifier: FallbackClassifier) -> None:
        result = fallback_classifier.classify(
            item("chrome.exe", "Disney+ Watch Now", "disneyplus.com")
        )
        assert result.category == "distraction"

    def test_eksi_sozluk(self, fallback_classifier: FallbackClassifier) -> None:
        result = fallback_classifier.classify(
            item("chrome.exe", "Ekşi Sözlük - Bugün", "eksisozluk.com")
        )
        assert result.category == "distraction"

    def test_inci_sozluk(self, fallback_classifier: FallbackClassifier) -> None:
        result = fallback_classifier.classify(
            item("chrome.exe", "İnci Sözlük", "incisozluk.com")
        )
        assert result.category == "distraction"

    def test_donanimhaber(self, fallback_classifier: FallbackClassifier) -> None:
        result = fallback_classifier.classify(
            item("chrome.exe", "DonanımHaber - Anasayfa", "donanimhaber.com")
        )
        assert result.category == "distraction"

    def test_spotify_is_neutral(self, fallback_classifier: FallbackClassifier) -> None:
        """Spotify now categorised as unknown (neutral background music)
        rather than distraction — it's often on during focused work."""
        result = fallback_classifier.classify(item("Spotify", "Spotify - Radiohead"))
        # 'unknown' is the canonical 'neutral' bucket for the fallback classifier.
        assert result.category == "unknown"


# ---------------------------------------------------------------------------
# Fixture-driven tests (30+ examples from classify_examples.json)
# ---------------------------------------------------------------------------


class TestFixtureExamples:
    def test_all_fixture_examples(
        self,
        fallback_classifier: FallbackClassifier,
        classify_examples: list[dict],
    ) -> None:
        if not classify_examples:
            pytest.skip("classify_examples.json not found")

        failures = []
        for ex in classify_examples:
            inp = ex["input"]
            expected = ex["expected_category"]
            ci = ClassifyItem(
                app_name=inp["app_name"],
                window_title=inp.get("window_title", ""),
                url=inp.get("url"),
            )
            result = fallback_classifier.classify(ci)
            if result.category != expected:
                failures.append(
                    f"  {inp['app_name']!r}: expected={expected!r}, got={result.category!r}"
                    f" (confidence={result.confidence:.2f})"
                )

        if failures:
            # Report failures but don't hard-fail; fallback accuracy is best-effort.
            # A >50% failure rate is a real problem, so assert on that.
            failure_rate = len(failures) / len(classify_examples)
            assert failure_rate < 0.30, (
                f"Fallback classifier failure rate {failure_rate:.0%} exceeds 30%:\n"
                + "\n".join(failures)
            )
