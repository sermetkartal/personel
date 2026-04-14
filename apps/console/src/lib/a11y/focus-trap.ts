/**
 * Lightweight focus-trap utility.
 *
 * Most of our modal surfaces use Radix primitives (Dialog, Popover, Sheet,
 * DropdownMenu) which ship focus-trap behaviour out of the box and should
 * be preferred. This module exists for the handful of custom containers
 * that need WCAG 2.1.2 (No Keyboard Trap) / 2.4.3 (Focus Order) compliance
 * without pulling the full `focus-trap` npm package.
 *
 * Usage:
 *   useEffect(() => {
 *     if (!containerRef.current) return;
 *     return trapFocus(containerRef.current);
 *   }, []);
 */

const FOCUSABLE_SELECTOR = [
  "a[href]",
  "area[href]",
  "button:not([disabled])",
  "input:not([disabled]):not([type='hidden'])",
  "select:not([disabled])",
  "textarea:not([disabled])",
  "iframe",
  "object",
  "embed",
  "[contenteditable='true']",
  "[tabindex]:not([tabindex='-1'])",
].join(", ");

export function getFocusableElements(container: HTMLElement): HTMLElement[] {
  const candidates = Array.from(
    container.querySelectorAll<HTMLElement>(FOCUSABLE_SELECTOR),
  );
  return candidates.filter((el) => {
    if (el.hasAttribute("disabled")) return false;
    if (el.getAttribute("aria-hidden") === "true") return false;
    // visible + occupies space
    const rect = el.getBoundingClientRect();
    if (rect.width === 0 && rect.height === 0) return false;
    return true;
  });
}

/**
 * Traps keyboard focus within `container` until the cleanup fn is called.
 * Returns a disposer that restores focus to the previously active element.
 *
 * Behaviour:
 * - Tab / Shift+Tab wraps within focusables
 * - First focusable gains focus on mount (unless `autoFocus` is false)
 * - `Escape` is NOT consumed — callers decide what to do on Esc
 */
export function trapFocus(
  container: HTMLElement,
  options: { autoFocus?: boolean } = {},
): () => void {
  const { autoFocus = true } = options;
  const previouslyFocused = document.activeElement as HTMLElement | null;

  function focusFirst() {
    const focusables = getFocusableElements(container);
    if (focusables.length > 0) {
      focusables[0]?.focus();
    } else {
      // Fall back to container — must be tabIndex=-1 for this to work.
      container.focus();
    }
  }

  function handleKeyDown(event: KeyboardEvent) {
    if (event.key !== "Tab") return;
    const focusables = getFocusableElements(container);
    if (focusables.length === 0) {
      event.preventDefault();
      return;
    }
    const first = focusables[0];
    const last = focusables[focusables.length - 1];
    const active = document.activeElement as HTMLElement | null;

    if (event.shiftKey) {
      if (active === first || !container.contains(active)) {
        event.preventDefault();
        last?.focus();
      }
    } else {
      if (active === last) {
        event.preventDefault();
        first?.focus();
      }
    }
  }

  if (autoFocus) {
    // Defer by one microtask so transitions/mounting finish first.
    queueMicrotask(focusFirst);
  }
  container.addEventListener("keydown", handleKeyDown);

  return () => {
    container.removeEventListener("keydown", handleKeyDown);
    if (previouslyFocused && typeof previouslyFocused.focus === "function") {
      previouslyFocused.focus();
    }
  };
}
