(() => {
  const header = document.querySelector("[data-site-header]");
  const menuToggle = document.querySelector("[data-menu-toggle]");
  const mobileNav = document.querySelector("[data-mobile-nav]");

  const updateHeader = () => {
    if (!header) return;
    header.classList.toggle("is-scrolled", window.scrollY > 8);
  };

  updateHeader();
  window.addEventListener("scroll", updateHeader, { passive: true });

  if (header && menuToggle && mobileNav) {
    const setMenu = (open) => {
      menuToggle.setAttribute("aria-expanded", String(open));
      menuToggle.setAttribute("aria-label", open ? "关闭导航" : "打开导航");
      mobileNav.hidden = !open;
      header.classList.toggle("is-open", open);
    };

    menuToggle.addEventListener("click", () => {
      setMenu(menuToggle.getAttribute("aria-expanded") !== "true");
    });

    mobileNav.addEventListener("click", (event) => {
      if (event.target.closest("a")) setMenu(false);
    });

    document.addEventListener("click", (event) => {
      if (!header.contains(event.target)) setMenu(false);
    });

    document.addEventListener("keydown", (event) => {
      if (event.key === "Escape" && menuToggle.getAttribute("aria-expanded") === "true") {
        setMenu(false);
        menuToggle.focus();
      }
    });

    window.matchMedia("(min-width: 861px)").addEventListener("change", (event) => {
      if (event.matches) setMenu(false);
    });
  }

  const platform = navigator.userAgentData?.platform || navigator.platform || "";
  let detectedOS = "";
  if (/mac/i.test(platform)) detectedOS = "macos";
  else if (/win/i.test(platform)) detectedOS = "windows";
  else if (/linux/i.test(platform) && !/android/i.test(platform)) detectedOS = "linux";

  if (detectedOS) {
    document.documentElement.dataset.os = detectedOS;
    document.querySelectorAll("[data-download-os]").forEach((row) => {
      row.classList.toggle("is-current", row.dataset.downloadOs === detectedOS);
    });
  }

  const showcase = document.querySelector("[data-showcase]");
  const reduceMotion = window.matchMedia("(prefers-reduced-motion: reduce)");
  const compactTour = window.matchMedia("(max-width: 860px)");
  if (!showcase || reduceMotion.matches || compactTour.matches) return;

  const stage = showcase.querySelector(".stage");
  const screens = Array.from(showcase.querySelectorAll(".tour-screen"));
  const captions = Array.from(showcase.querySelectorAll("[data-tour-caption]"));
  const ticks = Array.from(showcase.querySelectorAll("[data-tour-tick]"));
  const stepCount = Math.max(screens.length, captions.length);
  let queuedFrame = 0;
  let activeIndex = -1;

  const clamp = (value) => Math.min(1, Math.max(0, value));

  const setActiveCaption = (index) => {
    if (index === activeIndex) return;
    activeIndex = index;

    captions.forEach((caption, captionIndex) => {
      const current = captionIndex === index;
      caption.classList.toggle("is-current", current);
      if (current) caption.removeAttribute("aria-hidden");
      else caption.setAttribute("aria-hidden", "true");
    });

    screens.forEach((screen, screenIndex) => {
      if (screenIndex === index) screen.removeAttribute("aria-hidden");
      else screen.setAttribute("aria-hidden", "true");
    });

    ticks.forEach((tick, tickIndex) => {
      tick.classList.toggle("is-active", tickIndex === index);
      tick.classList.toggle("is-passed", tickIndex < index);
    });
  };

  const update = () => {
    queuedFrame = 0;
    const stageTop = parseFloat(getComputedStyle(stage).top) || 0;
    const sectionTop = showcase.getBoundingClientRect().top;
    const travel = Math.max(1, showcase.offsetHeight - stage.offsetHeight);
    const progress = clamp((stageTop - sectionTop) / travel);
    const story = progress * stepCount;
    const enterDistance = Math.max(1, showcase.offsetTop - stageTop);
    const enter = 1 - clamp((sectionTop - stageTop) / enterDistance);

    showcase.style.setProperty("--enter", enter.toFixed(4));

    screens.forEach((screen, index) => {
      if (index === 0) return;
      const reveal = clamp(story - index + 1);
      screen.style.setProperty("--t", reveal.toFixed(4));
    });

    const nextIndex = Math.min(captions.length - 1, Math.max(0, Math.floor(story + 0.5)));
    setActiveCaption(nextIndex);
  };

  const schedule = () => {
    if (!queuedFrame) queuedFrame = requestAnimationFrame(update);
  };

  showcase.classList.add("is-live");
  getComputedStyle(stage).top;
  update();
  window.addEventListener("scroll", schedule, { passive: true });
  window.addEventListener("resize", schedule);
  window.addEventListener("load", schedule);
})();
