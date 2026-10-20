// Set before React renders to avoid flashing the wrong palette.
export function initializeTheme() {
  const scheme = window.matchMedia("(prefers-color-scheme: dark)");
  const update = () => { document.documentElement.dataset.theme = scheme.matches ? "dark" : "light"; };
  update();
  scheme.addEventListener("change", update);
  return () => scheme.removeEventListener("change", update);
}
