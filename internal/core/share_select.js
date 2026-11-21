(() => {
  const form = document.getElementById('batch-download');
  const all = document.getElementById('select-all');
  const button = form.querySelector('button');
  const label = document.getElementById('download-label');
  const items = Array.from(document.querySelectorAll('input[name="item"][form="batch-download"]'));
  const passwords = Array.from(document.querySelectorAll('.batch-password input'), input => ({
    input, item: input.closest('li').querySelector('.pick input')
  }));

  function update() {
    const count = items.filter(item => item.checked).length;
    all.checked = count > 0 && count === items.length;
    all.indeterminate = count > 0 && count < items.length;
    all.disabled = items.length === 0;
    button.disabled = count === 0;
    label.textContent = count ? '下载所选（' + count + '）' : '下载所选';
    for (const {input, item} of passwords) {
      input.parentElement.hidden = !item.checked;
      input.disabled = !item.checked;
      input.required = item.checked;
      if (!item.checked) input.value = '';
    }
  }
  all.parentElement.hidden = false;
  all.addEventListener('change', () => {
    for (const item of items) item.checked = all.checked;
    update();
  });
  form.closest('.directory').addEventListener('change', update);
  addEventListener('pageshow', update);
  update();
})();
