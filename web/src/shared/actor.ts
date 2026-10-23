// Who decides. goald runs in single-user mode on a laptop, so the UI names the person from local
// storage (`goal.by`) and falls back to "web"; sign-in replaces this when goald gains OIDC.
const key = 'goal.by';

export function actor(): string {
  try {
    return localStorage.getItem(key)?.trim() || 'web';
  } catch {
    return 'web';
  }
}
