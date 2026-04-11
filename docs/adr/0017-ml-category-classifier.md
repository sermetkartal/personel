# ADR 0017 — ML Category Classifier via Local LLM

**Status**: Proposed for Phase 2. Not implemented.
**Deciders**: microservices-architect (Phase 2 planner); ratified at Phase 2 kickoff by backend-developer + ML engineer.
**Related**: ADR 0008 (on-prem first), ADR 0013 (off-by-default pattern), ADR 0005 (NATS), `docs/architecture/phase-2-scope.md` §B.4.

## Context

Phase 1 includes a rule-based (regex/allowlist) classifier in the policy engine that maps `(app_name, window_title, url)` to a coarse category (`work|personal|distraction|unknown`). It is functional and fast but brittle: Turkish-language application names and window titles are poorly covered, and every customer needs to tune the rule set.

Competitors (ActivTrak, Insightful) ship ML-backed category classifiers that learn from user activity. Their trick is sending the data to cloud inference — which is exactly what Personel's KVKK thesis forbids. Personel needs a classifier that is:

1. Accurate on Turkish-language application names and window titles.
2. Runs fully on customer hardware with zero cloud egress.
3. Explainable enough to survive KVKK m.4 / m.11 scrutiny (users can request and dispute the category label).
4. Customer-extensible — some customers have internal applications whose name means nothing to an off-the-shelf model.
5. Cheap enough to run on the existing on-prem server, not a dedicated GPU box.

## Decision

### Model: Llama 3.2 3B Instruct (quantized)

**Pick: Llama 3.2 3B Instruct, quantized AWQ 4-bit for CPU serving via llama.cpp.**

Reasoning:

- **Size**: Llama 3.1 8B would give ~5–8 point higher accuracy on the Turkish benchmark we expect to build, but at 3–4x the inference cost. On a 16-core Xeon server (typical Phase 1 target hardware), 3B gets us <50ms per classification, which is well under the streaming budget. 8B gets us ~200ms, which is still acceptable for batch but tight for interactive uses like the admin console drill-down.
- **License**: Llama 3.2 has a Meta community license that permits commercial use below 700M MAU. Personel is nowhere near that threshold. For customers who need a more permissive license, we support **Mistral 7B Instruct** or **Qwen 2.5 3B Instruct** as drop-in alternatives — the classifier interface is model-agnostic and the swap is a config change.
- **Turkish coverage**: Llama 3.2 was trained with more multilingual data than 3.1; it handles Turkish tokenization (BPE with pre-tokenization respecting Turkish diacritics) better. Preliminary testing on a 100-app test set (see `phase-2-exit-criteria.md` criterion C.8) targets **≥85% accuracy**.
- **Quantization**: AWQ 4-bit for CPU serving. Loss vs FP16 is ~1–2% accuracy on classification tasks. GGUF is the llama.cpp native format; we convert AWQ to GGUF (`q4_k_m` quant) for llama.cpp. Alternative: `llm-int8` via transformers, but llama.cpp is simpler to deploy and has no Python runtime dependency.
- **Backend**: **llama.cpp** (single C++ binary, no Python runtime at serving time). Python is only used in the calibration pipeline, not in production serving.

### Deployment topology

```
┌─────────────────────┐        ┌──────────────────────┐
│ enricher (Go)       │  NATS  │ ml-classifier (Go)   │
│ reads events.raw.*  │ ─────> │ batches events       │
│                     │        │ calls llama.cpp HTTP │
└─────────────────────┘        │ writes results to    │
                               │ events.enriched.*    │
                               └──────┬───────────────┘
                                      │ HTTP localhost
                                      ▼
                               ┌──────────────────────┐
                               │ llama-server         │
                               │ (llama.cpp -server)  │
                               │ ISOLATED NETWORK     │
                               │ no egress            │
                               └──────────────────────┘
```

- `ml-classifier` is a new Go container, sidecar-pattern with `llama-server`.
- Both containers run inside a Docker network segment **`net_ml`** which has **no default route** and explicit iptables rules dropping outbound traffic. Verified at install time by `preflight.sh`.
- `llama-server` listens on `127.0.0.1:8080` of the ml-classifier container only; the `ml-classifier` container is the only peer on that port.
- Model files (~2 GB for Llama 3.2 3B q4_k_m) are bundled in the container image for Phase 2.0; customers with air-gapped environments receive a signed `.tar.zst` bundle that `install.sh` stages into a named Docker volume `personel_ml_models`.
- Compose profile: `ml`. Activated by default in Phase 2 (unlike DLP which is off by default); classification is a low-risk enhancement, not a policy decision.

### Input and output contract

**Input** (per event, produced by `enricher` on a new NATS subject `events.to_classify.*`):

```json
{
  "event_id": "...",
  "tenant_id": "...",
  "app_name": "Microsoft Excel",
  "window_title": "2026-Q1-Bütçe.xlsx - Microsoft Excel",
  "url": null,
  "lang_hint": "tr"
}
```

**Output** (on `events.enriched.*`):

```json
{
  "event_id": "...",
  "category": "work",
  "confidence": 0.94,
  "model_version": "llama-3.2-3b-awq-20260501",
  "fallback_used": false
}
```

- **No raw screenshots, no OCR text, no keystroke content.** The classifier sees only metadata already captured under Phase 1 m.5/2-f legitimate interest. OCR text exists but goes to a different pipeline; cross-pipeline mixing is explicitly prohibited to keep m.6 risk bounded.
- Category enum: `work | personal | distraction | unknown | custom:<label>`.
- Confidence threshold: <0.70 → category is set to `unknown`, not written as `distraction`. This is a UX and KVKK decision: ambiguous classifications must not harm the user.

### Prompt template (system prompt)

Stable across model versions; versioned in repo:

```
You are a work activity classifier for a workplace analytics tool.
Given an application name and window title, classify the activity
into exactly one of: work, personal, distraction, unknown.
Respond with a JSON object: {"category": "...", "confidence": 0.0}.
Never produce other text.
If you cannot determine with confidence above 0.7, respond with unknown.
The user is in Turkey; Turkish-language applications and titles are common.
```

User prompt is filled with the event fields. Response parsed by a strict JSON validator; any malformed response is treated as `unknown`.

### Customer-specific calibration via LoRA

Some customers have internal applications — e.g., "MüşteriPortal v4", "Insight Analitik", "Logo Tiger 3" — that the base model has not seen. We provide a calibration path:

1. Customer admin (DPO + IT Security joint action, audit-logged) labels 50–200 internal applications in the Admin Console `/settings/ml-calibration` screen. Each label is `(app_name_regex, window_title_regex) → category`.
2. A calibration job runs nightly, generates a LoRA adapter (`peft` + `transformers` in the calibration container, one-time per update), quantizes it via `llama.cpp` LoRA format, and hot-loads it into `llama-server`.
3. Calibration data **stays on customer hardware**; no transmission back to Personel. LoRA training requires a modest compute burst (~30 min on CPU for 200 samples with 3B base model) but is off-hours.
4. Calibration data is personal data (it contains app names and window titles from a specific customer) and is itself subject to retention TTL (default 2 years, aligned with KVKK audit horizon).
5. This is a Phase 2 exit-criterion-optional feature: ship the interface and the calibration container, but the "auto-retrain on label submission" loop is a Phase 2.8 stretch goal; Phase 2 launch ships manual calibration only (admin submits labels, admin clicks "retrain").

### KVKK considerations

- **Legitimate interest (m.5/2-f)**: classification is a derived label from already-captured events. No new capture. The balance test remains favorable because: (a) no new data category, (b) classification improves the product's ability to serve its legitimate purpose (productivity analytics that the employer has a legitimate interest in), (c) the classifier is auditable.
- **Transparency (m.10)**: the portal surfaces each user's category distribution for the last 30 days with a "bu kategori yanlışsa itiraz et" button that creates a `category_dispute` DSR entry.
- **Right to object (m.11)**: the existing DSR framework handles this. A dispute flips the event-level category to `user_disputed` for that user's historical range; the admin console and reports exclude disputed categories from aggregates.
- **No cloud, no cross-tenant training**: architectural guarantee, not a policy promise. Verified at install time by preflight network check.
- **Explainability**: the classifier emits `(category, confidence, model_version, fallback_used)`. It does not emit a free-text reason because LLMs hallucinate reasons. For disputes, the admin console shows the exact inputs and the model version, which is sufficient evidence for a KVKK review.

### Fallback when the model is unavailable

If `llama-server` is unreachable (container crash, model file corrupt, OOM), the enricher falls back to the **Phase 1 rule-based classifier**. This is the exact same code path Phase 1 uses, now the fallback rather than the primary. Fallback events are tagged `fallback_used=true` so metrics can show the fallback rate; a sustained high fallback rate is a P1 alert.

## Consequences

### Positive

- Competitive ML-backed classification without compromising the KVKK thesis.
- Runs on existing Phase 1 hardware (no GPU required).
- Customer-extensible via LoRA without cross-customer data flow.
- Model swap is a config change; if Llama 3.2 licensing becomes problematic or a better open model appears, we move.
- Strong defensive story for KVKK DPO review.

### Negative / Risks

- **Latency budget is tight.** At 3B q4_k_m, per-event classification on a 16-core Xeon is ~40–60ms. At 1000 events/s (10k-endpoint target), we need batch inference (batch size 8–16) to meet throughput. Mitigated by the `ml-classifier` container's batch accumulator.
- **Disk footprint adds ~2 GB** for the model. Negligible.
- **Accuracy ceiling**: 85% is the Phase 2 target. Harder tails (e.g., browser-based SaaS apps that all render as "Google Chrome — Şirket — Google Chrome") will never hit 85% without URL inclusion. URL is already captured in Phase 1 but only at SNI granularity; OCR text could help but we explicitly do not cross-pipeline.
- **LLM hallucination risk**: the strict JSON parse contract and the confidence threshold address this, but it is not zero.
- **Legal review of Llama community license** must happen before pilot ship; engaged in Phase 2.0.

## Alternatives Considered

- **Cloud LLM (OpenAI, Anthropic, Azure OpenAI)**: rejected — KVKK thesis violation.
- **XGBoost on hand-engineered features**: evaluated; would work but requires a large labeled Turkish dataset we do not have. LLM approach gives us zero-shot Turkish capability for free.
- **Fine-tuned BERT classifier (e.g., `dbmdz/bert-base-turkish-cased`)**: evaluated; better inference speed than LLM (~5ms), but less flexible for customer calibration. Kept as a Phase 3 option if Phase 2 LLM approach has latency issues.
- **Llama 3.1 8B**: evaluated; better accuracy but worse latency. 8B remains a config option for customers with GPU-equipped servers.
- **Dedicated GPU**: rejected for Phase 2 default deployment; supported as an optional runtime for customers who provide one.
- **Per-event RAG over a Turkish app name knowledge base**: overkill for classification; the model already knows most apps.
- **Keep rule-based only**: rejected — competitive gap is too visible.

## Related

- `docs/architecture/phase-2-scope.md` §B.4
- `docs/architecture/c4-container-phase-2.md` (ml-classifier container)
- `docs/compliance/kvkk-framework.md` §2, §4 (legitimate interest, proportionality)
- `docs/adr/0013-dlp-disabled-by-default.md` (opt-in pattern reference)
