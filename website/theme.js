const toggle = document.querySelector("[data-theme-toggle]");
const systemTheme = window.matchMedia("(prefers-color-scheme: dark)");
const root = document.documentElement;
const screenshots = document.querySelectorAll("source[data-dark]");
const preferenceKey = "goim-website-theme";

function applyTheme(theme) {
  if (theme) {
    root.dataset.theme = theme;
  } else {
    delete root.dataset.theme;
  }
  const dark = theme ? theme === "dark" : systemTheme.matches;
  for (const screenshot of screenshots) {
    screenshot.media = dark ? "all" : "not all";
  }
  toggle.setAttribute("aria-pressed", String(dark));
}

let preference;
try {
  const saved = localStorage.getItem(preferenceKey);
  if (saved === "light" || saved === "dark") preference = saved;
} catch {
  // A browser may block local storage.
}
applyTheme(preference);
toggle.hidden = false;
toggle.addEventListener("click", () => {
  preference = toggle.getAttribute("aria-pressed") === "true" ? "light" : "dark";
  applyTheme(preference);
  try {
    localStorage.setItem(preferenceKey, preference);
  } catch {
    // The theme still applies for this page.
  }
});
systemTheme.addEventListener("change", () => {
  if (!preference) applyTheme();
});
