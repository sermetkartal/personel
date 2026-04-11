"""
Prompt construction for the Llama 3.2 3B Instruct classifier.

The prompt is English-dominant (the model is English-dominant) but the
few-shot examples include Turkish application names and window titles that
are common in the Turkish business software ecosystem.

Prompt versioning: the canonical text lives in prompts/classify_v1.txt.
build_prompt() loads it from disk at first call (cached).  Tests can
override _PROMPT_TEMPLATE directly.
"""

from __future__ import annotations

import json
from functools import lru_cache
from pathlib import Path
from typing import TYPE_CHECKING

if TYPE_CHECKING:
    from personel_ml.schemas import ClassifyItem

# Relative to this file: src/personel_ml/prompt.py → ../../prompts/classify_v1.txt
_PROMPT_FILE = Path(__file__).parent.parent.parent.parent / "prompts" / "classify_v1.txt"

# Injected by tests to skip disk access.
_PROMPT_TEMPLATE: str | None = None


@lru_cache(maxsize=1)
def _load_system_prompt() -> str:
    if _PROMPT_TEMPLATE is not None:
        return _PROMPT_TEMPLATE
    if _PROMPT_FILE.exists():
        return _PROMPT_FILE.read_text(encoding="utf-8").strip()
    # Inline fallback if the file is somehow missing at runtime.
    return _INLINE_SYSTEM_PROMPT.strip()


def build_prompt(item: "ClassifyItem") -> str:
    """Build the complete llama.cpp prompt for a single ClassifyItem.

    Returns a string in the Llama 3 Instruct chat format:
      <|begin_of_text|><|start_header_id|>system<|end_header_id|>
      {system_prompt}
      <|eot_id|><|start_header_id|>user<|end_header_id|>
      {user_prompt}
      <|eot_id|><|start_header_id|>assistant<|end_header_id|>

    The model is expected to complete only the JSON object.
    """
    system_prompt = _load_system_prompt()

    url_part = item.url if item.url else "null"
    user_content = json.dumps(
        {
            "app_name": item.app_name,
            "window_title": item.window_title,
            "url": url_part,
        },
        ensure_ascii=False,
    )

    return (
        "<|begin_of_text|>"
        "<|start_header_id|>system<|end_header_id|>\n\n"
        f"{system_prompt}"
        "<|eot_id|>"
        "<|start_header_id|>user<|end_header_id|>\n\n"
        f"{user_content}"
        "<|eot_id|>"
        "<|start_header_id|>assistant<|end_header_id|>\n\n"
    )


def extract_json_from_response(raw: str) -> dict:
    """Extract and parse the first valid JSON object from the model output.

    The model is instructed to emit ONLY a JSON object, but LLMs occasionally
    prepend whitespace, markdown fences, or prose.  This function is tolerant.

    Returns an empty dict on any parse failure; the caller treats that as
    unknown/0.0 confidence.
    """
    raw = raw.strip()
    # Strip markdown code fence if present.
    if raw.startswith("```"):
        lines = raw.splitlines()
        raw = "\n".join(
            line for line in lines if not line.startswith("```")
        ).strip()

    # Find the first { ... } block.
    start = raw.find("{")
    end = raw.rfind("}")
    if start == -1 or end == -1 or end < start:
        return {}

    candidate = raw[start : end + 1]
    try:
        return json.loads(candidate)
    except json.JSONDecodeError:
        return {}


# ---------------------------------------------------------------------------
# Inline system prompt fallback (identical to prompts/classify_v1.txt)
# Kept here so the service is self-contained if the file mount is missing.
# ---------------------------------------------------------------------------

_INLINE_SYSTEM_PROMPT = """You are a work activity classifier for a workplace analytics tool.

Your task: given an application name, window title, and optional URL, classify the activity into EXACTLY ONE of these categories:
  - work         : productive, job-related activity
  - personal     : personal use unrelated to work (personal email, banking, shopping)
  - distraction  : entertainment/leisure that reduces work focus (social media, video streaming, games)
  - unknown      : cannot determine with sufficient confidence

Rules:
1. Respond with ONLY a JSON object: {"category": "...", "confidence": 0.0}
2. Never produce any other text, explanation, or markdown.
3. confidence must be a float between 0.0 and 1.0.
4. If confidence is below 0.70, set category to "unknown".
5. The user works in Turkey; Turkish-language application names and window titles are common.
6. Turkish business applications (Logo Tiger, Mikro Gold, Netsis, Paraşüt, BordroPlus, e-Fatura) are always "work".

Examples:
{"app_name": "Microsoft Excel", "window_title": "2026-Q1-Bütçe.xlsx", "url": null}
{"category": "work", "confidence": 0.97}

{"app_name": "Logo Tiger 3", "window_title": "Logo Tiger - Fatura Modülü", "url": null}
{"category": "work", "confidence": 0.99}

{"app_name": "chrome.exe", "window_title": "YouTube - Komedi Videoları", "url": "youtube.com"}
{"category": "distraction", "confidence": 0.95}

{"app_name": "chrome.exe", "window_title": "Stack Overflow - Python async guide", "url": "stackoverflow.com"}
{"category": "work", "confidence": 0.93}

{"app_name": "Slack", "window_title": "# genel - Ekip kanalı", "url": null}
{"category": "work", "confidence": 0.91}

{"app_name": "Instagram", "window_title": "Instagram - Keşfet", "url": "instagram.com"}
{"category": "distraction", "confidence": 0.96}

{"app_name": "Netflix", "window_title": "Stranger Things - Netflix", "url": "netflix.com"}
{"category": "distraction", "confidence": 0.98}

{"app_name": "chrome.exe", "window_title": "ING Bank - Hesabım", "url": "ingbank.com.tr"}
{"category": "personal", "confidence": 0.88}

Now classify the following item:"""
