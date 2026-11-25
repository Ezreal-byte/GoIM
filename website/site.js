const page = document.documentElement;
const themeButtons = document.querySelectorAll(".theme");

function showTheme() {
  const dark = page.dataset.theme !== "light";
  for (const button of themeButtons) button.setAttribute("aria-pressed", String(dark));
  document.querySelector('meta[name="theme-color"]').content = dark ? "#1a1a1a" : "#ffffff";
}
showTheme();
for (const button of themeButtons) {
  button.addEventListener("click", () => {
    page.dataset.theme = page.dataset.theme === "light" ? "dark" : "light";
    try { localStorage.setItem("goim-theme", page.dataset.theme); } catch {}
    showTheme();
  });
}

const menus = document.querySelectorAll(".tools details");
document.addEventListener("click", (event) => {
  for (const menu of menus) {
    if (menu.open && (!menu.contains(event.target) || event.target.closest("a"))) menu.open = false;
  }
});
document.addEventListener("keydown", (event) => {
  if (event.key !== "Escape") return;
  for (const menu of menus) {
    if (!menu.open) continue;
    menu.open = false;
    menu.querySelector("summary").focus();
  }
});

const search = document.querySelector("#search-dialog");
const input = search.querySelector("input");
const results = [...search.querySelectorAll("li")];
function filterResults() {
  const words = input.value.trim().toLocaleLowerCase().split(/\s+/);
  let matches = 0;
  for (const item of results) {
    const text = (item.textContent + " " + item.dataset.keywords).toLocaleLowerCase();
    item.hidden = !words.every((word) => text.includes(word));
    if (!item.hidden) matches++;
  }
  search.querySelector(".empty").hidden = matches > 0;
}
function openSearch() {
  if (!search.open) search.showModal();
  input.value = "";
  filterResults();
  input.focus();
}
document.querySelector(".search-open").addEventListener("click", openSearch);
input.addEventListener("input", filterResults);
search.querySelector("button").addEventListener("click", () => search.close());
search.addEventListener("click", (event) => {
  if (event.target.closest("a")) search.close();
  if (event.target !== search) return;
  const rect = search.getBoundingClientRect();
  if (event.clientX < rect.left || event.clientX > rect.right || event.clientY < rect.top || event.clientY > rect.bottom) search.close();
});
document.addEventListener("keydown", (event) => {
  if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === "k") {
    event.preventDefault();
    if (search.open) search.close();
    else openSearch();
  }
});
search.addEventListener("keydown", (event) => {
  const links = results.filter((item) => !item.hidden).map((item) => item.firstElementChild);
  const index = links.indexOf(document.activeElement);
  if (event.key === "ArrowDown" && links.length) {
    event.preventDefault();
    links[Math.min(index + 1, links.length - 1)].focus();
  } else if (event.key === "ArrowUp" && index >= 0) {
    event.preventDefault();
    (links[index - 1] || input).focus();
  } else if (event.key === "Enter" && document.activeElement === input && links.length) {
    event.preventDefault();
    links[0].click();
  }
});

function enableTour(tour) {
  const slides = [...tour.querySelectorAll(".slide")];
  const stage = tour.querySelector(".stage");
  const caption = tour.querySelector(".tour-caption");
  const motion = matchMedia("(prefers-reduced-motion: reduce)");
  const clamp = (value) => Math.max(0, Math.min(1, value));
  let active = -1;
  let frame = 0;

  function render() {
    frame = 0;
    const navHeight = document.querySelector("header").offsetHeight;
    const top = tour.getBoundingClientRect().top - navHeight;
    const enter = clamp(1 - top / Math.max(1, top + scrollY));
    const progress = clamp(-top / Math.max(1, tour.offsetHeight - stage.offsetHeight)) * (slides.length - 1);
    tour.style.setProperty("--enter", enter.toFixed(4));
    let current = 0;
    for (let i = 1; i < slides.length; i++) {
      const amount = clamp((progress - (i - 1) - .2) / .6);
      const smooth = amount * amount * (3 - 2 * amount);
      slides[i].style.setProperty("--reveal", (smooth * 100).toFixed(2) + "%");
      if (i === 1) slides[i].style.setProperty("--circle", (smooth * 145).toFixed(2) + "%");
      if (amount >= .5) current = i;
    }
    if (current !== active) {
      const slide = slides[current];
      caption.querySelector("strong").textContent = slide.dataset.title;
      caption.querySelector("p").textContent = slide.dataset.caption;
      slides.forEach((item, index) => {
        item.setAttribute("aria-hidden", String(index !== current));
        item.tabIndex = index === current ? 0 : -1;
      });
      active = current;
    }
    caption.style.opacity = String(enter);
  }
  function schedule() {
    if (!frame) frame = requestAnimationFrame(render);
  }
  function setMotion() {
    cancelAnimationFrame(frame);
    frame = 0;
    active = -1;
    tour.classList.toggle("live", !motion.matches);
    if (motion.matches) {
      window.removeEventListener("scroll", schedule);
      window.removeEventListener("resize", schedule);
      slides[0].removeAttribute("aria-hidden");
      slides[0].tabIndex = 0;
      return;
    }
    tour.style.setProperty("--travel", (slides.length - 1) * 60 + "svh");
    window.addEventListener("scroll", schedule, { passive: true });
    window.addEventListener("resize", schedule);
    render();
  }
  motion.addEventListener("change", setMotion);
  setMotion();
}
const tour = document.querySelector(".tour");
if (tour) {
  enableTour(tour);
  const followDownloadBookmark = () => {
    if (location.hash === "#download") location.replace(document.getElementById("download").href);
  };
  followDownloadBookmark();
  window.addEventListener("hashchange", followDownloadBookmark);
}

const downloadPage = document.querySelector(".platforms");
let releaseRefresh;
let lastLiveCheck = 0;
if (downloadPage) {
  const platform = navigator.userAgentData?.platform || navigator.platform || "";
  const mobile = /Android|iPhone|iPad|iPod/i.test(navigator.userAgent)
    || (/Mac/i.test(platform) && navigator.maxTouchPoints > 1);
  if (!mobile) {
    const os = /Win/i.test(platform) ? "windows" : /Mac/i.test(platform) ? "macos" : /Linux/i.test(platform) ? "linux" : "";
    if (os) document.querySelector("#" + os + " h2 small").hidden = false;
  }
  refreshPackages();
  window.addEventListener("focus", refreshPackages);
  document.addEventListener("visibilitychange", () => {
    if (!document.hidden) refreshPackages();
  });
  // The same-origin release snapshot has no GitHub API quota or connectivity requirement.
  window.setInterval(() => {
    if (!document.hidden) refreshPackages();
  }, 60000);
}

function refreshPackages() {
  if (releaseRefresh) return releaseRefresh;
  releaseRefresh = updatePackages().catch(() => {}).finally(() => { releaseRefresh = undefined; });
  return releaseRefresh;
}

async function updatePackages() {
  const { fetchLatestRelease, parseRelease, releaseForPage, snapshotURL } = await import("./release.mjs?v=1");
  const snapshot = fetch(snapshotURL, { cache: "no-store", signal: AbortSignal.timeout(5000) })
    .then((response) => {
      if (!response.ok) throw new Error("Release snapshot unavailable");
      return response.json();
    }).then(parseRelease);
  const checkLive = Date.now() - lastLiveCheck >= 300000;
  if (checkLive) lastLiveCheck = Date.now();
  // Check GitHub on arrival and periodically, without consuming its public quota every minute.
  const [live, saved] = await Promise.allSettled([checkLive ? fetchLatestRelease() : Promise.reject(), snapshot]);
  const versionLink = document.querySelector(".release-version");
  const release = releaseForPage(live.status === "fulfilled" ? live.value : null,
    saved.status === "fulfilled" ? saved.value : null, versionLink.querySelector("span").textContent);
  if (!release) return;
  const version = release.tag_name.slice(1);
  const links = [...downloadPage.querySelectorAll(".package-link")];
  const assets = links.map((link) => release.assets.find((asset) => asset.name === `GoIM_${version}_${link.dataset.package}`));
  if (!assets.every(Boolean)) return;
  links.forEach((link, index) => { link.href = assets[index].browser_download_url; });
  versionLink.href = release.html_url;
  versionLink.querySelector("span").textContent = release.tag_name;
}
