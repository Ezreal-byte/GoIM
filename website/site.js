const gallery = document.querySelector(".gallery-links");
const preview = document.getElementById("screen");
const original = document.getElementById("screen-link");
const status = document.getElementById("screen-status");

gallery.addEventListener("click", (event) => {
  const choice = event.target.closest("a");
  if (!choice || event.ctrlKey || event.metaKey || event.shiftKey || event.altKey) return;
  event.preventDefault();
  if (choice.hasAttribute("aria-current")) return;

  gallery.querySelector("[aria-current]")?.removeAttribute("aria-current");
  choice.setAttribute("aria-current", "true");
  preview.src = choice.href;
  preview.alt = choice.dataset.alt;
  original.href = choice.href;
  status.textContent = choice.dataset.caption;
});

const menu = document.querySelector(".mobile-menu");
document.addEventListener("click", (event) => {
  if (menu.open && (!menu.contains(event.target) || event.target.closest("a"))) menu.open = false;
});
document.addEventListener("keydown", (event) => {
  if (event.key === "Escape" && menu.open) {
    menu.open = false;
    menu.querySelector("summary").focus();
  }
});

const platform = navigator.userAgentData?.platform || navigator.platform || "";
const mobile = /Android|iPhone|iPad|iPod/i.test(navigator.userAgent)
  || (/Mac/i.test(platform) && navigator.maxTouchPoints > 1);
if (!mobile) {
  const systems = { windows: /Win/i, macos: /Mac/i, linux: /Linux/i };
  for (const row of document.querySelectorAll("[data-os]")) {
    if (systems[row.dataset.os].test(platform)) row.querySelector(".detected").hidden = false;
  }
}
