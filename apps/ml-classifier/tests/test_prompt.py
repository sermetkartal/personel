"""
Unit tests for prompt construction and JSON extraction.

Validates that build_prompt() produces the correct Llama 3 chat format and
that extract_json_from_response() handles edge cases gracefully.
"""

from __future__ import annotations

import pytest

from personel_ml.prompt import _INLINE_SYSTEM_PROMPT, build_prompt, extract_json_from_response
from personel_ml.schemas import ClassifyItem


def _item(app: str, title: str = "", url: str | None = None) -> ClassifyItem:
    return ClassifyItem(app_name=app, window_title=title, url=url)


# ---------------------------------------------------------------------------
# build_prompt tests
# ---------------------------------------------------------------------------


class TestBuildPrompt:
    def test_contains_llama3_chat_tokens(self) -> None:
        prompt = build_prompt(_item("Excel", "Budget.xlsx"))
        assert "<|begin_of_text|>" in prompt
        assert "<|start_header_id|>system<|end_header_id|>" in prompt
        assert "<|start_header_id|>user<|end_header_id|>" in prompt
        assert "<|start_header_id|>assistant<|end_header_id|>" in prompt

    def test_contains_app_name(self) -> None:
        prompt = build_prompt(_item("Logo Tiger 3", "Fatura Modulu"))
        assert "Logo Tiger 3" in prompt

    def test_contains_window_title(self) -> None:
        prompt = build_prompt(_item("chrome.exe", "Stack Overflow - Python asyncio"))
        assert "Stack Overflow" in prompt

    def test_null_url_appears_as_null(self) -> None:
        prompt = build_prompt(_item("Excel", "Budget.xlsx", url=None))
        assert '"url": "null"' in prompt

    def test_url_included_when_provided(self) -> None:
        prompt = build_prompt(_item("chrome.exe", "GitHub", "github.com"))
        assert "github.com" in prompt

    def test_ends_with_assistant_header(self) -> None:
        prompt = build_prompt(_item("Slack", "# engineering"))
        assert prompt.endswith("<|start_header_id|>assistant<|end_header_id|>\n\n")

    def test_system_prompt_is_non_empty(self) -> None:
        prompt = build_prompt(_item("Slack"))
        # System content is between system header and eot_id
        assert len(prompt) > 200

    def test_inline_fallback_prompt_contains_rules(self) -> None:
        assert "work" in _INLINE_SYSTEM_PROMPT
        assert "personal" in _INLINE_SYSTEM_PROMPT
        assert "distraction" in _INLINE_SYSTEM_PROMPT
        assert "unknown" in _INLINE_SYSTEM_PROMPT
        assert "0.70" in _INLINE_SYSTEM_PROMPT

    def test_inline_fallback_contains_turkish_apps(self) -> None:
        assert "Logo Tiger" in _INLINE_SYSTEM_PROMPT
        assert "Mikro" in _INLINE_SYSTEM_PROMPT or "BordroPlus" in _INLINE_SYSTEM_PROMPT


# ---------------------------------------------------------------------------
# extract_json_from_response tests
# ---------------------------------------------------------------------------


class TestExtractJsonFromResponse:
    def test_clean_json(self) -> None:
        raw = '{"category": "work", "confidence": 0.92}'
        result = extract_json_from_response(raw)
        assert result == {"category": "work", "confidence": 0.92}

    def test_json_with_leading_whitespace(self) -> None:
        raw = '   \n{"category": "distraction", "confidence": 0.95}\n'
        result = extract_json_from_response(raw)
        assert result["category"] == "distraction"

    def test_json_inside_markdown_fence(self) -> None:
        raw = '```json\n{"category": "personal", "confidence": 0.88}\n```'
        result = extract_json_from_response(raw)
        assert result["category"] == "personal"

    def test_json_with_trailing_prose(self) -> None:
        raw = '{"category": "work", "confidence": 0.97} I think this is work-related.'
        result = extract_json_from_response(raw)
        assert result["category"] == "work"

    def test_malformed_json_returns_empty_dict(self) -> None:
        raw = "category: work, confidence: 0.9"
        result = extract_json_from_response(raw)
        assert result == {}

    def test_empty_string_returns_empty_dict(self) -> None:
        result = extract_json_from_response("")
        assert result == {}

    def test_no_braces_returns_empty_dict(self) -> None:
        result = extract_json_from_response("I cannot determine the category.")
        assert result == {}

    def test_nested_json_extracts_outer(self) -> None:
        # Model might theoretically wrap the response
        raw = '{"category": "work", "confidence": 0.85, "extra": {"key": "val"}}'
        result = extract_json_from_response(raw)
        assert result["category"] == "work"
        assert result["confidence"] == 0.85

    def test_confidence_float_preserved(self) -> None:
        raw = '{"category": "unknown", "confidence": 0.42}'
        result = extract_json_from_response(raw)
        assert abs(result["confidence"] - 0.42) < 1e-9

    def test_integer_confidence_is_ok(self) -> None:
        raw = '{"category": "work", "confidence": 1}'
        result = extract_json_from_response(raw)
        assert result["confidence"] == 1
