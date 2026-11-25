export const releasesURL = "https://github.com/Ezreal-byte/GoIM/releases";
export const snapshotURL = new URL("./release.json", import.meta.url);
export const packageFiles = [
  "windows_amd64-Setup.exe", "windows_arm64-Setup.exe", "windows_amd64.zip", "windows_arm64.zip",
  "darwin_amd64.dmg", "darwin_arm64.dmg", "darwin_amd64.zip", "darwin_arm64.zip",
  "linux_amd64.deb", "linux_arm64.deb", "linux_amd64.tar.gz", "linux_arm64.tar.gz",
];

export function parseRelease(value) {
  if (!value || value.draft || value.prerelease || !/^v\d+\.\d+\.\d+$/.test(value.tag_name)) {
    throw new Error("Expected a published, stable GoIM release");
  }
  const version = value.tag_name.slice(1);
  const assets = packageFiles.map((file) => {
    const name = `GoIM_${version}_${file}`;
    const url = `${releasesURL}/download/${value.tag_name}/${name}`;
    const asset = value.assets?.find((item) => item.name === name && item.browser_download_url === url);
    if (!asset) throw new Error(`Missing release package: ${name}`);
    return { name, browser_download_url: url };
  });
  return {
    tag_name: value.tag_name, html_url: `${releasesURL}/tag/${value.tag_name}`,
    published_at: value.published_at, draft: false, prerelease: false, assets,
  };
}

export function releaseForPage(live, snapshot, currentTag) {
  if (live || !snapshot || !/^v\d+\.\d+\.\d+$/.test(currentTag)) return live || snapshot;
  const current = currentTag.slice(1).split(".").map(Number);
  const saved = snapshot.tag_name.slice(1).split(".").map(Number);
  for (let i = 0; i < current.length; i++) {
    if (saved[i] < current[i]) return null;
    if (saved[i] > current[i]) return snapshot;
  }
  return snapshot;
}

export async function fetchLatestRelease({ fetcher = fetch, token } = {}) {
  async function request(path) {
    const headers = { Accept: "application/vnd.github+json" };
    if (token) headers.Authorization = `Bearer ${token}`;
    const response = await fetcher(`https://api.github.com/repos/Ezreal-byte/GoIM/releases${path}`, {
      headers, cache: "no-store", signal: AbortSignal.timeout(8000),
    });
    if (!response.ok) throw new Error(`GitHub release request failed: ${response.status}`);
    return response.json();
  }
  try {
    return parseRelease(await request("/latest"));
  } catch (error) {
    // The repository also publishes independently versioned helper releases.
    // Never mistake those, previews or incomplete uploads for the GoIM client.
    const releases = await request("?per_page=30");
    for (const release of releases) {
      try { return parseRelease(release); } catch {}
    }
    throw error;
  }
}
