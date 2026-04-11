# Personel

**Personel**, Türkiye pazarı için geliştirilen, KVKK uyumlu, on-prem öncelikli kurumsal çalışan aktivite izleme ve performans analitiği platformudur. Rust tabanlı Windows uç nokta ajanı, Go tabanlı mikroservisler, Next.js yönetici konsolu ve kriptografik olarak izole edilmiş bir DLP servisinden oluşur.

Ürün, Teramind / ActivTrak / Veriato / Insightful / Safetica çözümlerinin yerli alternatifi olarak konumlanır. Temel farklılaşma:

- **KVKK-first tasarım**: 6698 sayılı Kanun ve VERBİS şablonlarına bağlı saklama matrisi.
- **Admin-blind klavye içeriği**: Yöneticiler klavye içeriğini **kriptografik olarak** göremez; sadece izole DLP servisi pattern eşleşmesi için açabilir.
- **Çift kontrollü canlı izleme**: İK onayı olmadan hiçbir canlı ekran oturumu başlayamaz; her adım hash-zincirli denetim kaydına yazılır.
- **On-prem öncelik**: Docker Compose + systemd ile tek komutla kurulur; Kubernetes gerektirmez.

---

## Developer Quickstart (English)

> Phase 0 is **architecture only** — there is no runnable code yet. This section will grow as specialist agents land implementations.

### Repository Layout

```
personel/
├── apps/
│   ├── agent/        # Rust endpoint agent (Windows, user-mode, tokio/tonic)
│   ├── gateway/      # Go ingest gateway (mTLS, gRPC bidi stream, NATS publisher)
│   ├── api/          # Go admin API (tenants, policies, live-view, reports)
│   ├── console/      # Next.js admin console (App Router)
│   ├── portal/       # Next.js employee transparency portal
│   └── dlp/          # Go DLP service (isolated; keystroke decryption + pattern match)
├── packages/
│   └── proto/        # Generated proto stubs (Go, TS, Rust)
├── proto/
│   └── personel/v1/  # Canonical protobuf schemas (source of truth)
├── infra/
│   ├── compose/      # Docker Compose stacks (on-prem deploy)
│   └── systemd/      # systemd units for host-level services
├── docs/
│   ├── architecture/ # C4, bounded contexts, MVP scope, event taxonomy, crypto
│   ├── adr/          # Architecture Decision Records (0001–0010)
│   └── security/     # Threat model, anti-tamper
└── README.md
```

### Getting Started as a Downstream Contributor

1. **Read** `docs/README.md` for a full doc index.
2. **Read** `docs/architecture/overview.md` (Turkish exec summary) and `docs/architecture/mvp-scope.md` (English Phase 1 scope).
3. **Read** every ADR in `docs/adr/` — they are short and load-bearing.
4. **Read** `docs/architecture/key-hierarchy.md` if you touch any code path that sees keystroke data. It is non-negotiable.
5. **Consult** `proto/personel/v1/` for the wire contract. Do not diverge without a new ADR.

### Prerequisites (planned; not yet wired up)

- Rust 1.80+ (MSVC target on Windows for agent release builds)
- Go 1.22+
- Node 20+, pnpm 9+
- protoc + buf
- Docker 24+, docker-compose v2
- HashiCorp Vault 1.16+
- ClickHouse 24+, PostgreSQL 16+, NATS 2.10+, MinIO latest, OpenSearch 2.x, LiveKit 1.x

### Build (planned)

```bash
# Generate proto stubs (once tooling lands)
buf generate

# Per-component builds
cargo build -p personel-agent --release
go build ./apps/gateway/...
pnpm --filter @personel/console build
```

### Deploying On-Prem (planned)

```bash
# Initialize Vault, issue CAs
./infra/scripts/bootstrap.sh

# Start the stack
docker compose -f infra/compose/docker-compose.yml up -d

# Enable host services
sudo systemctl enable --now personel-compose.service
```

---

## Status

- **Phase 0 — Architecture**: complete (this commit).
- **Phase 1 — Pilot implementation**: not started.
- **Phase 2 — macOS/Linux agent, OCR, ML**: not started.
- **Phase 3 — Minifilter driver, SaaS, Kubernetes**: not started.

## License

Proprietary. All rights reserved.
