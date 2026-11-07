// Responsive browser QA for the smoke test's ephemeral, localhost-only share.
import { spawn } from "node:child_process";
import { readFile, writeFile } from "node:fs/promises";
import { join } from "node:path";
import { setTimeout as delay } from "node:timers/promises";

const [chrome, profile, url, output] = process.argv.slice(2);
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
