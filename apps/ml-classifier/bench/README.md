# Phase 2 Benchmark Harness — ml-classifier

**Status**: Scaffolded. Not implemented. Implementation target: Phase 2 sprint before pilot.

## Purpose

Measure classification accuracy against the 100-app Turkish test set defined in
`docs/architecture/phase-2-scope.md` criterion C.8 (target: >=85% accuracy for
Llama 3.2 3B q4_k_m on the Turkish benchmark).

## Planned approach

### Input dataset

`bench/data/turkish_app_benchmark_100.jsonl` — 100 hand-labelled examples covering:
- Turkish ERP suite apps (Logo Tiger, Mikro, Netsis, BordroPlus, Paraşüt)
- Turkish government apps (e-Devlet, KEP, MERNİS client, SGK desktop)
- Turkish banking web apps (Garanti, İş, Akbank, YKB, Ziraat)
- Common international productivity tools (Office, Slack, Teams, Zoom, GitHub)
- Distractions (YouTube, Netflix, Instagram, TikTok, Steam)
- Personal apps (Gmail, WhatsApp personal, Trendyol, Hepsiburada)
- Ambiguous apps (browser new tab, generic notepad, custom internal apps)

### Benchmark script

`bench/run_benchmark.py` (to be written):

```python
# Pseudocode
for example in load_jsonl("bench/data/turkish_app_benchmark_100.jsonl"):
    result = httpx.post("http://localhost:8080/v1/classify", json=example["input"])
    record(expected=example["label"], actual=result.json()["category"])

print_confusion_matrix()
print_accuracy_by_category()
assert overall_accuracy >= 0.85, "Phase 2 exit criterion C.8 FAILED"
```

### Latency measurement

The benchmark additionally records p50/p95/p99 classify latency and asserts:
- p99 < 100ms on a 16-core Xeon @ 2.5 GHz (single-item, no batch)
- p50 < 50ms (ADR 0017 latency budget)

### Running against alternative models

The benchmark supports `PERSONEL_ML_MODEL_PATH` and `PERSONEL_ML_MODEL_VERSION`
env vars to swap models without changing code. Mistral 7B Instruct and
Qwen 2.5 3B Instruct are the two planned alternatives (ADR 0017).

### CI integration

In Phase 2 CI, the benchmark runs weekly against the staged model volume.
A sustained drop below 85% triggers a Slack alert and blocks the Phase 2
accuracy gate from closing.

## Dependencies (not yet installed)

```
httpx
tqdm
scikit-learn  # for confusion matrix and classification report
pandas
```

## Open questions for devops-engineer

1. Where is the ground-truth benchmark dataset stored? (repo vs artifact store)
2. Is the 100-example set sufficient, or do we need 500+ for statistical significance?
3. Should the benchmark run as a separate container in the `ml` compose profile?
