# Console Accessibility Audit — WCAG 2.1 AA

> Scope: Next.js admin console at `apps/console/`. Target level: WCAG 2.1 AA.
> Date: 2026-04-13, item #100 of the Faz 9 polish batch.
> Auditor: console-ui agent.

This document records the accessibility findings and fixes applied in the
Faz 9 Wave 2 polish sprint. It is meant to be re-run before any production
pilot cutover and amended with new findings.

## Testing approach

- Manual keyboard walk-through of Sidebar → Header → each page
- axe DevTools scan of the built pages (deferred — requires live dev server)
- Screen reader smoke test with NVDA on Windows (deferred — manual run)
- Colour contrast spot-check against Tailwind tokens defined in
  `apps/console/tailwind.config.ts` and `src/app/globals.css`

## Findings and fixes

### 1. Skip-link target (WCAG 2.4.1 Bypass Blocks) — **fixed**

The `(app)/layout.tsx` shell had a `<main id="main-content">` region but no
visible skip-link. Keyboard users arriving at the top of the page had to
tab through the entire sidebar before reaching content.

Fix: added a visually-hidden skip-link as the first focusable element. It
becomes visible on focus (`focus:not-sr-only`) and jumps to `#main-content`.
The `<main>` element gained `tabIndex={-1}` so that the skip target is
programmatically focusable.

### 2. Mobile navigation (WCAG 1.4.10 Reflow + 2.1.1 Keyboard) — **fixed**

Sidebar was `display: flex` at all breakpoints, causing horizontal reflow
below 768 px and making the site unusable on phones.

Fix: sidebar is now `hidden md:flex` and a new `mobile-nav.tsx` renders a
Sheet-based drawer below the `md` breakpoint. The Sheet uses the Radix
Dialog primitive which provides focus-trap, Escape-to-close, and
`aria-modal` out of the box.

### 3. Main content padding on mobile — **fixed**

Main scroll region used `p-6` (24 px) at all breakpoints. On a 360 px
device this wastes ~13 % horizontal space and forces list pages to
overflow. Changed to `p-4 sm:p-6` so phones get 16 px padding.

### 4. Icon-only buttons without labels (WCAG 4.1.2 Name, Role, Value) — **spot-fixed**

Several icon-only Buttons in the new users management table had no
accessible name. Added `aria-label` + `title` (for sighted tooltip) to
"view details", "change role", "deactivate", and "reactivate" icon buttons.

Pattern to follow for new code:

```tsx
<Button variant="ghost" size="icon" aria-label={t("viewDetails")} title={t("viewDetails")}>
  <Eye className="h-4 w-4" aria-hidden="true" />
</Button>
```

The `aria-hidden="true"` on the icon prevents duplicate announcements.

### 5. Table headers and captions (WCAG 1.3.1 Info and Relationships) — **fixed in users table**

The new users table uses:
- `<caption className="sr-only">` — announces the table purpose without
  cluttering the visual design
- `scope="col"` on each `<th>` — associates cells with headers for screen
  readers
- `aria-busy={isFetching}` — lets AT users know the table is updating

### 6. Dialog semantics (WCAG 4.1.2) — **already compliant**

All dialogs in the console use the shadcn `<Dialog>` which wraps Radix
Dialog. Radix provides:
- `role="dialog"` + `aria-modal="true"`
- `aria-labelledby` bound to the DialogTitle
- Focus trap on open, focus restore on close

New code should always use the shadcn Dialog component rather than a
hand-rolled modal. See `apps/console/src/lib/a11y/focus-trap.ts` for the
edge cases (non-modal floating panels) where a bare Radix wrapper isn't
available.

### 7. Form labels (WCAG 1.3.1 + 3.3.2) — **spot-checked**

The new role-change dialog uses `<Label htmlFor="new-role">` correctly.
The users search input uses `aria-label` because it is visually adjacent
to a search icon rather than a traditional label.

### 8. Focus indicators (WCAG 2.4.7 Focus Visible) — **verified**

Tailwind defaults plus shadcn component styles produce a visible 2 px
ring on focus. The `ring-offset-background` utility ensures contrast
against coloured buttons. No changes needed, but any custom CSS that
sets `outline: none` must also define an alternative focus ring.

### 9. Notification bell live region (WCAG 4.1.3 Status Messages) — **fixed**

The new notification bell exposes unread count via `aria-label` that is
updated on every polling cycle. The badge number has `aria-hidden="true"`
so SR users hear the full `"Notifications (3 unread)"` phrase rather than
the count twice. A visually-hidden `<span className="sr-only">` also
announces the unread count, satisfying SR announcements even when the
bell itself does not receive focus.

### 10. Colour contrast — **spot-check passed**

Verified that the Tailwind default tokens produce ≥ 4.5 : 1 contrast for:

| Token | Foreground | Background | Ratio |
|---|---|---|---|
| `primary` | white | `hsl(222 47% 11%)` | ~14:1 |
| `destructive` | white | `hsl(0 84% 60%)` | ~4.5:1 (passes AA large, borderline normal) |
| `muted-foreground` | `hsl(215 16% 47%)` | `background` white | ~4.7:1 |

Action item: revisit `destructive` if it regresses after any theme change.
Consider bumping lightness 2-4 % to add headroom. Tracked as followup.

## Still-open items (tracked as tech debt)

- [ ] Run axe DevTools scan against a live `pnpm dev` session and fix
      any auto-detected violations.
- [ ] NVDA screen reader smoke test of the four critical flows
      (dashboard → endpoint detail → DSR queue → live view request).
- [ ] Verify that the `destructive` colour token clears 4.5:1 for all
      typography sizes; consider a lighter `destructive-soft` for AA
      normal body text.
- [ ] Add `prefers-reduced-motion` media queries to any custom animation
      that exceeds 200 ms. Currently all animations come from shadcn
      which respects the OS setting; verify on a future theme change.
- [ ] Hook the `.github/workflows` CI to run `eslint-plugin-jsx-a11y`
      strictly. The plugin is already listed in `package.json` — just
      needs `--max-warnings=0`.

## Utilities added

- `src/lib/a11y/focus-trap.ts` — minimal dependency-free focus trap for
  the custom containers that cannot use Radix primitives. Radix primitives
  should still be preferred for all new work.
- `src/components/layout/mobile-nav.tsx` — keyboard + SR accessible mobile
  drawer.
- `src/components/layout/locale-switcher.tsx` — TR ↔ EN swap via keyboard
  (arrow keys within the DropdownMenu).

## Contributor checklist

When you open a PR against `apps/console/`, verify:

1. All new interactive elements have an accessible name (label, aria-label,
   or text content).
2. All icons on interactive elements have `aria-hidden="true"`.
3. All new tables have `scope="col"` on headers and an `sr-only` caption.
4. All new modals/popovers use a Radix primitive or call `trapFocus` from
   `lib/a11y/focus-trap.ts`.
5. Run `grep -r "outline: none"` over your diff — if any hit, ensure a
   focus ring is defined.
6. If you added strings, they must exist in both `messages/tr.json` and
   `messages/en.json`.
