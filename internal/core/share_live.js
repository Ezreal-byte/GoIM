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
      // Keep pending uploads, download selections and their passwords intact.
      const dialog = document.querySelector('#upload-dialog');
      function whenIdle() {
        if (dialog?.open) dialog.addEventListener('close', whenIdle, {once:true});
        else if (dialog?.dataset.uploadActive === 'true') dialog.addEventListener('uploadidle', whenIdle, {once:true});
        else if (document.querySelector('.pick input:checked')) document.querySelector('.selectable').addEventListener('change', whenIdle, {once:true});
        else refresh();
      }
      whenIdle();
    });
  }

  document.addEventListener('visibilitychange', watch);
  addEventListener('pagehide', stop);
  addEventListener('pageshow', watch);
  watch();
})();
