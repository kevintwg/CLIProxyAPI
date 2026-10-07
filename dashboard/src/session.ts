// Remembered management keys live in this browser's localStorage. Storage can be
// unavailable (private modes, blocked cookies), so every access fails quietly and
// the dashboard falls back to keeping the key in memory only.
const storageKey = "relay.managementKey";

export function readRememberedKey(): string | null {
  try {
    const value = window.localStorage.getItem(storageKey);
    return value && value.trim() ? value : null;
  } catch {
    return null;
  }
}

export function rememberKey(key: string): void {
  try {
    window.localStorage.setItem(storageKey, key);
  } catch {
    // Storage unavailable: the key stays in memory for this tab only.
  }
}

export function forgetKey(): void {
  try {
    window.localStorage.removeItem(storageKey);
  } catch {
    // Storage unavailable: nothing was saved.
  }
}
