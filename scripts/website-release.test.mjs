import assert from "node:assert/strict";
import { mkdir, mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { pathToFileURL } from "node:url";
import test from "node:test";
import { fetchLatestRelease, packageFiles, parseRelease, releaseForPage, releasesURL } from "../website/release.mjs";
import { syncWebsiteRelease } from "./website-release.mjs";

function fixture(tag = "v0.2.6") {
  return {
    tag_name: tag, draft: false, prerelease: false, published_at: "2026-11-25T09:00:00Z",
    assets: packageFiles.map((file) => {
      const name = `GoIM_${tag.slice(1)}_${file}`;
      return { name, browser_download_url: `${releasesURL}/download/${tag}/${name}` };
    }),
  };
}

test("a stable client release supplies all six architectures and twelve packages", () => {
  const result = parseRelease(fixture());
  assert.equal(result.tag_name, "v0.2.6");
  assert.equal(result.assets.length, 12);
  assert.equal(result.html_url, `${releasesURL}/tag/v0.2.6`);
});

test("drafts, previews, helpers, incomplete packages and foreign download URLs are rejected", () => {
  const incomplete = fixture();
  incomplete.assets.pop();
  const foreign = fixture();
  foreign.assets[0].browser_download_url = "https://example.com/installer.exe";
  for (const invalid of [null, { ...fixture(), draft: true }, { ...fixture(), prerelease: true },
    fixture("v0.2.7-beta.1"), fixture("goim-rd-v0.1.0"), incomplete, foreign]) {
    assert.throws(() => parseRelease(invalid));
  }
});

test("normal latest releases need one uncached API request", async () => {
  const requested = [];
  const result = await fetchLatestRelease({ fetcher: async (url, options) => {
    requested.push(url);
    assert.equal(options.cache, "no-store");
    return { ok: true, json: async () => fixture() };
  } });
  assert.equal(result.tag_name, "v0.2.6");
  assert.equal(requested.length, 1);
  assert.ok(requested[0].endsWith("/latest"));
});

test("an independently published helper never replaces the client download", async () => {
  let requests = 0;
  const result = await fetchLatestRelease({ fetcher: async () => ({
    ok: true,
    json: async () => ++requests === 1 ? fixture("goim-rd-v0.1.0") : [
      fixture("goim-rd-v0.1.0"), { ...fixture("v0.2.7"), draft: true },
      { ...fixture("v0.2.7"), prerelease: true }, fixture(),
    ],
  }) });
  assert.equal(result.tag_name, "v0.2.6");
  assert.equal(requests, 2);
});

test("API rate limits report failure so the browser can retain the local snapshot", async () => {
  await assert.rejects(fetchLatestRelease({ fetcher: async () => ({ ok: false, status: 403 }) }), /403/);
});

test("a delayed Pages deployment cannot undo a newer version already obtained from GitHub", () => {
  const snapshot = parseRelease(fixture());
  assert.equal(releaseForPage(null, snapshot, "v0.2.7"), null);
  assert.equal(releaseForPage(null, snapshot, "v0.2.5"), snapshot);
  assert.equal(releaseForPage(null, snapshot, "latest"), snapshot);
  assert.equal(releaseForPage(snapshot, null, "v0.2.7"), snapshot);
});

test("deployment embeds the current release into both languages, including without JavaScript", async (t) => {
  const temporary = await mkdtemp(join(tmpdir(), "goim-website-release-"));
  t.after(() => rm(temporary, { recursive: true, force: true }));
  const root = pathToFileURL(temporary + "/");
  for (const route of ["download", "en/download"]) {
    await mkdir(new URL(route + "/", root), { recursive: true });
    await writeFile(new URL(route + "/index.html", root),
      await readFile(new URL(`../website/${route}/index.html`, import.meta.url)));
  }
  await syncWebsiteRelease(root, fixture("v0.3.0"));
  for (const route of ["download", "en/download"]) {
    const html = await readFile(new URL(route + "/index.html", root), "utf8");
    assert.match(html, /<span>v0\.3\.0<\/span>/);
    assert.equal((html.match(/releases\/download\/v0\.3\.0\/GoIM_0\.3\.0_/g) || []).length, 12);
    assert.doesNotMatch(html, /v0\.2\.4/);
  }
  const saved = parseRelease(JSON.parse(await readFile(new URL("release.json", root), "utf8")));
  assert.equal(saved.tag_name, "v0.3.0");
});
