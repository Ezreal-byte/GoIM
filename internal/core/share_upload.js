(() => {
  const dialog = document.querySelector('#upload-dialog');
  if (!dialog) return;

  const input = document.querySelector('#upload-file');
  const form = document.querySelector('#upload-form');
  const selection = document.querySelector('#upload-selection');
  const clear = document.querySelector('#clear-upload');
  const submit = form.querySelector('button[type=submit]');
  const drop = document.querySelector('#upload-drop');
  const open = document.querySelector('[data-open-upload]');
  const close = document.querySelector('[data-close-upload]');
  const status = document.querySelector('#upload-status');
  const maxBytes = Number(form.dataset.limit);

  function updateSelection() {
    const file = input.files[0];
    selection.textContent = file ? file.name : '尚未选择文件';
    clear.hidden = !file;
    status.textContent = file && file.size > maxBytes ? '文件超过 256 MB，请更换文件。' : '';
    submit.disabled = !file || Boolean(status.textContent);
  }

  function show() {
    if (!dialog.open) dialog.showModal();
  }

  open.addEventListener('click', event => {
    if (event.ctrlKey || event.metaKey || event.altKey || event.shiftKey || !dialog.showModal) return;
    event.preventDefault();
    show();
  });
  close.addEventListener('click', event => {
    if (!dialog.matches(':modal')) return;
    event.preventDefault();
    dialog.close();
  });
  input.addEventListener('change', updateSelection);
  clear.addEventListener('click', () => {
    input.value = '';
    updateSelection();
    input.focus();
  });
  for (const eventName of ['dragenter', 'dragover', 'dragleave', 'drop']) {
    drop.addEventListener(eventName, event => {
      if (!event.dataTransfer?.types.includes('Files')) return;
      event.preventDefault();
      drop.classList.toggle('dragging', eventName === 'dragenter' || eventName === 'dragover');
      if (eventName !== 'drop') return;
      show();
      if (event.dataTransfer.files.length !== 1) {
        input.value = '';
        updateSelection();
        status.textContent = '每次只能上传一个文件。';
        return;
      }
      input.files = event.dataTransfer.files;
      updateSelection();
    });
  }
  form.addEventListener('submit', () => {
    submit.disabled = true;
    submit.textContent = '正在上传…';
    form.setAttribute('aria-busy', 'true');
  });
  window.addEventListener('pageshow', () => {
    form.removeAttribute('aria-busy');
    submit.textContent = '开始上传';
    updateSelection();
  });
  updateSelection();
})();
