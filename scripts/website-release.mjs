import { readFile, writeFile } from "node:fs/promises";
import { fileURLToPath, pathToFileURL } from "node:url";
import { fetchLatestRelease, packageFiles, parseRelease } from "../website/release.mjs";

export async function syncWebsiteRelease(root, release) {
  release = parseRelease(release);
  for (const route of ["download/index.html", "en/download/index.html"]) {
    const path = new URL(route, root);
    let html = await readFile(path, "utf8");
    let versionCount = 0;
    html = html.replace(/(<a class="release-version" href=")[^"]+(">[^\n]*?<span>)[^<]*(<\/span>)/g,
      (_, before, middle, after) => {
        versionCount++;
        return before + release.html_url + middle + release.tag_name + after;
      });
    const found = new Set();
    html = html.replace(/(<a class="package-link" data-package="([^"]+)" href=")[^"]+(")/g,
      (_, before, file, after) => {
        if (!packageFiles.includes(file)) throw new Error(`Unknown package: ${file}`);
        found.add(file);
        const asset = release.assets.find((item) => item.name === `GoIM_${release.tag_name.slice(1)}_${file}`);
        return before + asset.browser_download_url + after;
      });
    if (versionCount !== 1 || found.size !== packageFiles.length) throw new Error(`Incomplete download page: ${route}`);
    await writeFile(path, html, "utf8");
  }
  await writeFile(new URL("release.json", root), JSON.stringify(release, null, 2) + "\n", "utf8");
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  const root = new URL("../website/", import.meta.url);
  // An optional captured response allows reproducible, offline verification.
  const release = process.argv[2]
    ? parseRelease(JSON.parse(await readFile(process.argv[2], "utf8")))
    : await fetchLatestRelease({ token: process.env.GH_TOKEN });
  await syncWebsiteRelease(root, release);
  console.log(`Website downloads synchronized to ${release.tag_name}: ${fileURLToPath(root)}`);
}
