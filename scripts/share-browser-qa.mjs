// Responsive browser QA for the smoke test's ephemeral, localhost-only share.
import { spawn } from "node:child_process";
import { readFile, unlink, writeFile } from "node:fs/promises";
import { join } from "node:path";
import { setTimeout as delay } from "node:timers/promises";

const [chrome, profile, url, output, folderURL, sharedDirectory, deviceURL] = process.argv.slice(2);
if (!chrome || !profile || !output || !/^http:\/\/127\.0\.0\.1:\d+\/s\/[\w-]+$/.test(url ?? "")) {
  throw new Error("Expected Chrome path, isolated profile, loopback share URL, and output directory");
}
const browser = spawn(chrome, [
  "--headless=new", "--no-first-run", "--disable-extensions", "--disable-background-networking",
  "--remote-debugging-port=0", `--user-data-dir=${profile}`, "about:blank",
], { stdio: "ignore", windowsHide: true });
let socket;
try {
  const port = await eventually(async () =>
    (await readFile(join(profile, "DevToolsActivePort"), "utf8")).split("\n")[0]);
  const page = await fetch(`http://127.0.0.1:${port}/json/new?about:blank`, {method:"PUT"}).then(r => r.json());
  socket = new WebSocket(page.webSocketDebuggerUrl);
  await new Promise((resolve, reject) => {
    socket.addEventListener("open", resolve, {once:true});
    socket.addEventListener("error", reject, {once:true});
  });
  let sequence = 0;
  const pending = new Map();
  socket.addEventListener("message", ({data}) => {
    const reply = JSON.parse(data);
    const result = pending.get(reply.id);
    if (!result) return;
    pending.delete(reply.id);
    if (reply.error) result.reject(new Error(reply.error.message));
    else result.resolve(reply.result);
  });
  const call = (method, params = {}) => new Promise((resolve, reject) => {
    const id = ++sequence;
    pending.set(id, {resolve, reject});
    socket.send(JSON.stringify({id, method, params}));
  });
  const evaluate = async (expression) => {
    const reply = await call("Runtime.evaluate", {expression, returnByValue:true});
    if (reply.exceptionDetails) throw new Error(reply.exceptionDetails.text);
    return reply.result.value;
  };
  const capture = async (name) => {
    if (!(await evaluate("innerWidth === 390 && document.documentElement.scrollWidth <= innerWidth"))) {
      throw new Error("Share page overflows a 390 px mobile viewport");
    }
    const {data} = await call("Page.captureScreenshot", {format:"png", captureBeyondViewport:false});
    await writeFile(join(output, name), Buffer.from(data, "base64"));
  };
  await call("Page.enable");
  await call("Emulation.setDeviceMetricsOverride", {width:390, height:760, deviceScaleFactor:1, mobile:true});
  await call("Page.navigate", {url});
  await eventually(() => evaluate("!!document.querySelector('form input[type=password]')"));
  await capture("browser-share-password.png");
  await evaluate("document.querySelector('input').value='goim-share-smoke'; document.querySelector('form').requestSubmit(); true");
  await eventually(() => evaluate("Array.from(document.links).some(a => a.pathname.endsWith('/download'))"));
  await capture("browser-share-download.png");
  await call("Emulation.setDeviceMetricsOverride", {width:1440,height:900,deviceScaleFactor:1,mobile:false});
  await call("Page.navigate", {url:folderURL});
  await eventually(() => evaluate("!!document.querySelector('a.entry .file-type')"));
  const metrics = await evaluate(`(() => {
    const row=document.querySelector('a.entry'), bounds=row.getBoundingClientRect();
    const icon=getComputedStyle(row.querySelector('.file-type'));
    return {width:bounds.width, height:bounds.height, gutter:bounds.left, icon:icon.backgroundImage+icon.maskImage};
  })()`);
  if (metrics.width>1040 || metrics.height>44 || metrics.gutter<32 || !metrics.icon.includes('svg')) {
    throw new Error("Directory geometry or file icon regression: "+JSON.stringify(metrics));
  }
  await call("Browser.setDownloadBehavior", {behavior:"allow", downloadPath:profile});
  const verifyBatch = async (device) => {
    await eventually(() => evaluate("!document.querySelector('.batch-all').hidden && document.querySelector('#batch-download button').disabled"));
    await evaluate("document.querySelector('.pick input').click()");
    if (!(await evaluate("document.querySelector('#select-all').indeterminate && document.querySelector('#download-label').textContent.includes('1')"))) {
      throw new Error("Partial selection did not update the download button");
    }
    await evaluate("document.querySelector('#select-all').click()");
    if (!(await evaluate("document.querySelector('#select-all').checked && !document.querySelector('.pick input:not(:checked)')"))) {
      throw new Error("Select all left an unchecked row");
    }
    if (device) {
      await evaluate("document.querySelector('.batch-password input').value='goim-share-smoke'");
      await capture("browser-share-multiselect-mobile.png");
    }
    await evaluate("document.querySelector('#batch-download button').click()");
    const archive = join(profile, 'GoIM-所选文件.zip');
    await eventually(async () => {
      const file = await readFile(archive);
      return file.readUInt32LE(0) === 0x04034b50 && file.readUInt32LE(file.length-22) === 0x06054b50;
    });
    await unlink(archive);
    await evaluate("document.querySelector('#select-all').click()");
  };
  await verifyBatch(false);
  if (sharedDirectory) {
    const liveName = 'live-browser-' + process.pid + '.txt';
    const testFile = join(sharedDirectory, liveName);
    const listed = () => evaluate("Array.from(document.querySelectorAll('.filename'), e => e.textContent).includes(" + JSON.stringify(liveName) + ")");
    await writeFile(testFile, 'browser live update', {flag:'wx'});
    try {
      await eventually(listed);
    } finally {
      await unlink(testFile);
    }
    await eventually(async () => (await evaluate("!!document.querySelector('.directory')")) && !(await listed()));
  }
  const {data}=await call("Page.captureScreenshot",{format:"png",captureBeyondViewport:false});
  await writeFile(join(output,"browser-share-directory.png"),Buffer.from(data,"base64"));
  if (deviceURL) {
    await call("Page.navigate",{url:deviceURL});
    await eventually(() => evaluate("Boolean(document.querySelector('[data-open-upload]'))"));
    await call("Emulation.setDeviceMetricsOverride",{width:390,height:844,deviceScaleFactor:1,mobile:true});
    await verifyBatch(true);
    await call("Emulation.setDeviceMetricsOverride",{width:1440,height:900,deviceScaleFactor:1,mobile:false});
    await evaluate("document.querySelector('[data-open-upload]').click(); true");
    const width = await evaluate("document.querySelector('#upload-dialog').getBoundingClientRect().width");
    if (width < 600) throw new Error("Desktop upload dialog is too narrow");
    await call("Emulation.setDeviceMetricsOverride",{width:390,height:844,deviceScaleFactor:1,mobile:false});
    if (!(await evaluate("document.querySelector('#upload-dialog').getBoundingClientRect().width <= innerWidth && document.querySelector('#upload-target').textContent.trim() === '选择文件或将文件拖到这里'"))) {
      throw new Error("Mobile text-only upload target does not fit");
    }
    await capture("browser-web-upload-mobile.png");
    await call("Page.setInterceptFileChooserDialog",{enabled:true});
    let chooser;
    const onChooser = event => {
      const msg=JSON.parse(event.data);
      if (msg.method === "Page.fileChooserOpened") chooser=msg.params;
    };
    socket.addEventListener("message",onChooser);
    await evaluate("document.querySelector('#upload-file').focus()");
    await call("Input.dispatchKeyEvent",{type:"keyDown",key:"Enter",code:"Enter",text:"\r",unmodifiedText:"\r",windowsVirtualKeyCode:13});
    await call("Input.dispatchKeyEvent",{type:"keyUp",key:"Enter",code:"Enter",windowsVirtualKeyCode:13});
    await eventually(() => chooser?.mode === "selectMultiple");
    await call("DOM.setFileInputFiles",{backendNodeId:chooser.backendNodeId,files:[]});
    chooser = undefined;
    const point = await evaluate("(() => {const r=document.querySelector('#upload-target span').getBoundingClientRect();return {x:r.x+r.width/2,y:r.y+r.height/2}})()");
    await call("Input.dispatchMouseEvent",{type:"mousePressed",...point,button:"left",clickCount:1});
    await call("Input.dispatchMouseEvent",{type:"mouseReleased",...point,button:"left",clickCount:1});
    await eventually(() => chooser?.mode === "selectMultiple");
    await call("DOM.setFileInputFiles",{backendNodeId:chooser.backendNodeId,files:[]});
    socket.removeEventListener("message",onChooser);
    await call("Page.setInterceptFileChooserDialog",{enabled:false});
    await call("Emulation.setDeviceMetricsOverride",{width:1440,height:900,deviceScaleFactor:1,mobile:false});
    // Drop inside the modal (the background directory is inert while it is open).
    await evaluate("(() => {const d = new DataTransfer(); for (const name of ['browser-first.txt','browser-second.txt']) d.items.add(new File([name],name));document.querySelector('#upload-target').dispatchEvent(new DragEvent('drop',{bubbles:true,cancelable:true,dataTransfer:d}));document.querySelector('#upload-form').requestSubmit();return true;})()");
    await eventually(() => evaluate("Array.from(document.querySelectorAll('#upload-list .file-status')).filter(e => e.textContent==='已接收').length===2"));
    const upload = await call("Page.captureScreenshot",{format:"png",captureBeyondViewport:false});
    await writeFile(join(output,"browser-web-uploads.png"),Buffer.from(upload.data,"base64"));
  }
} finally {
  socket?.close();
  browser.kill();
}

async function eventually(check) {
  for (let attempt = 0; attempt < 100; attempt++) {
    try {
      const result = await check();
      if (result) return result;
    } catch {
      // Chrome may be between documents, or the isolated profile not ready.
    }
    await delay(100);
  }
  throw new Error("Timed out waiting for the browser share page");
}
