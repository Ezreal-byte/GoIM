(() => {
  const dialog = document.querySelector('#upload-dialog');
  if (!dialog) return;
  const form = dialog.querySelector('form');
  const picker = form.querySelector('input[type=file]');
  const queue = form.querySelector('#upload-list');
  const clear = form.querySelector('#clear-upload');
  const submit = form.querySelector('button[type=submit]');
  const status = form.querySelector('#upload-status');
  const endpoint = form.action;
  const rows = [];
  const limit = Number(form.dataset.limit);
  const wait = ms => new Promise(resolve => setTimeout(resolve, ms));
  let running = 0;
  form.classList.add('enhanced');
  picker.required = false;

  function buttons() {
    submit.disabled = !rows.some(row => row.phase === 'selected');
    clear.hidden = !rows.some(row => ['selected','finished'].includes(row.phase));
    dialog.dataset.uploadActive = String(rows.some(row => ['queued','active'].includes(row.phase)));
  }
  function show() {
    if (!dialog.open) dialog.showModal();
  }
  function render(row, message, percent, failed = false) {
    row.label.textContent = message;
    row.label.classList.toggle('failed', failed);
    row.bar.value = percent;
    row.bar.setAttribute('aria-valuetext', message);
  }
  function add(files) {
    for (const file of files) {
      const item = document.createElement('li');
      const name = document.createElement('span');
      name.className = 'file-name';
      name.textContent = file.name;
      const label = document.createElement('span');
      label.className = 'file-status';
      const bar = document.createElement('progress');
      bar.max = 100;
      bar.setAttribute('aria-label', file.name);
      item.append(name, label, bar);
      queue.append(item);
      const row = {file, label, bar, item, phase:'selected'};
      rows.push(row);
      if (file.size > limit) {
        row.phase = 'finished';
        render(row, '超过 256 MB', 0, true);
      } else {
        render(row, '待发送', 0);
      }
    }
    picker.value = '';
    buttons();
  }
  async function request(url, options) {
    const response = await fetch(url, {...options, cache:'no-store'});
    if (!response.ok) throw new Error(response.status === 404 ? '设备主页已关闭或请求已失效' : (await response.text()).trim() || '请求失败');
    return response.json();
  }
  function send(row, url) {
    return new Promise((resolve, reject) => {
      const xhr = new XMLHttpRequest();
      xhr.open('PUT', url);
      xhr.timeout = 10 * 60 * 1000;
      xhr.upload.onprogress = e => {
        const percent = e.total ? Math.round(e.loaded * 100 / e.total) : 0;
        render(row, percent === 100 ? '正在保存' : '上传中 ' + percent + '%', percent);
      };
      xhr.onload = () => {
        if (xhr.status === 204) return resolve();
        reject(Object.assign(new Error(xhr.responseText.trim() || '接收失败'), {status:xhr.status}));
      };
      xhr.onerror = () => reject(new Error('连接中断'));
      xhr.ontimeout = () => reject(new Error('上传超时'));
      xhr.send(row.file);
    });
  }
  async function upload(row) {
    try {
      render(row, '正在请求接收', 0);
      let ticket = await request(endpoint + '/request', {method:'POST', headers:{'Content-Type':'application/json'}, body:JSON.stringify({name:row.file.name, size:row.file.size})});
      const url = endpoint + '/' + encodeURIComponent(ticket.id);
      // Confirmation is a metadata exchange. File bytes leave the browser only after approval.
      for (;;) {
        if (ticket.status === 'offered') {
          render(row, '等待 GoIM 同意', 0);
        } else if (ticket.status === 'waiting') {
          render(row, '上传中 0%', 0);
          try {
            await send(row, url);
            break;
          } catch (error) {
            if (error.status === 429) render(row, '接收方忙，排队中', 0);
            else if (error.status !== 403) {
              const latest = await request(url).catch(() => null);
              if (latest?.status === 'cancelled') throw new Error('接收方已取消');
              if (latest?.status === 'completed') break;
              throw error;
            }
          }
        } else {
          throw new Error(({rejected:'接收方已拒绝',cancelled:'接收方已取消',failed:'请求已过期或中断'})[ticket.status] || '请求已失效');
        }
        await wait(1100);
        ticket = await request(url);
      }
      row.received = true;
      render(row, '已接收', 100);
    } catch (error) {
      render(row, error.message || '上传失败', row.bar.value, true);
    } finally {
      row.phase = 'finished';
      running--;
      startQueue();
    }
  }
  function startQueue() {
    for (const row of rows) {
      if (running >= 2) break;
      if (row.phase !== 'queued') continue;
      row.phase = 'active';
      running++;
      upload(row);
    }
    buttons();
    if (!running) {
      const finished = rows.filter(row => row.phase === 'finished');
      const received = finished.filter(row => row.received).length;
      status.textContent = '已接收 ' + received + ' 个文件' + (received < finished.length ? '，未接收 ' + (finished.length - received) + ' 个' : '');
      if (!dialog.open) dialog.dispatchEvent(new Event('uploadidle'));
    }
  }
  document.querySelector('[data-open-upload]').addEventListener('click', e => {
    if (e.ctrlKey || e.metaKey || e.shiftKey || e.altKey || !dialog.showModal) return;
    e.preventDefault(); show();
  });
  dialog.querySelector('[data-close-upload]').addEventListener('click', e => {e.preventDefault(); dialog.close();});
  picker.addEventListener('change', () => add(picker.files));
  clear.addEventListener('click', () => {
    for (let i = rows.length - 1; i >= 0; i--) {
      if (!['selected','finished'].includes(rows[i].phase)) continue;
      rows[i].item.remove();
      rows.splice(i, 1);
    }
    status.textContent = ''; buttons(); picker.focus();
  });
  for (const target of [document.querySelector('#upload-drop'), dialog]) {
    const highlight = target === dialog ? dialog.querySelector('#upload-target') : target;
    for (const type of ['dragenter','dragover','dragleave','drop']) {
      target.addEventListener(type, e => {
        if (!Array.from(e.dataTransfer?.types || []).includes('Files')) return;
        e.preventDefault();
        if (type === 'dragleave' && target.contains(e.relatedTarget)) return;
        highlight.classList.toggle('dragging', type === 'dragenter' || type === 'dragover');
        if (type === 'drop') {show(); add(e.dataTransfer.files);}
      });
    }
  }
  form.addEventListener('submit', e => {
    e.preventDefault(); status.textContent = '';
    for (const row of rows) {
      if (row.phase === 'selected') {row.phase = 'queued'; render(row, '排队中', 0);}
    }
    startQueue();
  });
  buttons();
})();
