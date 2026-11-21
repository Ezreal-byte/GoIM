(() => {
  const script = document.currentScript;
  if (!window.EventSource || !script) return;
  const project = new URL(script.dataset.source, location.origin);
  const url = new URL(project.pathname + '/events', location.origin);
  url.searchParams.set('v', script.dataset.version);
  let stream;
  let stale = false;

  function stop() {
    stream?.close();
    stream = undefined;
  }

  function watch() {
    if (document.hidden) return stop();
    if (stream || stale) return;
    stream = new EventSource(url);
    stream.addEventListener('change', () => {
      stop();
      stale = true;
      const refresh = () => {
        if (project.pathname.startsWith('/d/')) location.replace(project.href);
        else location.reload();
      };
      // Keep the user's selected upload until the dialog is closed.
      const dialog = document.querySelector('dialog[open]');
      if (dialog) dialog.addEventListener('close', refresh, {once: true});
      else refresh();
    });
  }

  document.addEventListener('visibilitychange', watch);
  addEventListener('pagehide', stop);
  addEventListener('pageshow', watch);
  watch();
})();
