# Tests — Deferred

Test suite is deferred per the MVP scope specification.

When implementing:

- Use Vitest + React Testing Library for unit/component tests
- Use Playwright for e2e tests
- Priority test targets:
  - `NewRequestForm` — DSR submission flow, validation
  - `FirstLoginModal` — cannot dismiss without clicking Anladım, audit call fires
  - `SessionHistoryList` — restricted state, empty state, populated state
  - `DlpBanner` — off state rendering, on state rendering with date
  - `RequestTimeline` — SLA progress calculation, overdue state
  - Middleware — auth redirect behaviour, locale routing
  - API client — scope enforcement (non-/v1/me paths rejected)
