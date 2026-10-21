export type Theme = "dark" | "light";
const themeKey = "pactify-theme";
export function initializeTheme() {
  let theme: Theme = "dark";
  try { if (localStorage.getItem(themeKey) === "light") theme = "light"; } catch { /* Storage may be disabled. */ }
  document.documentElement.dataset.theme = theme;
  return () => {};
}
export function setTheme(theme: Theme) {
  document.documentElement.dataset.theme = theme;
  try { localStorage.setItem(themeKey, theme); } catch { /* Keep the in-memory choice. */ }
  window.dispatchEvent(new Event("pactify-theme-change"));
}
export function storedChoice(key: string, fallback: string) {
  try { return localStorage.getItem(key) || fallback; } catch { return fallback; }
}
export function saveChoice(key: string, value: string) {
  try { localStorage.setItem(key, value); } catch { /* Preferences are optional. */ }
}
