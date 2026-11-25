#!/usr/bin/env python3
"""Publish and consume versioned GoIM remote desktop helper bundles."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import stat
import subprocess
import tempfile
import zipfile

ROOT = Path(__file__).resolve().parent.parent
TARGETS = tuple(f"{platform}-{arch}" for platform in ("windows", "darwin", "linux") for arch in ("amd64", "arm64"))
BUILD_WORKFLOW = ".github/workflows/remote-desktop-build.yml"
LEGACY_WORKFLOW = ".github/workflows/remote-desktop.yml"
RECIPE_FILES = ("packaging/remote-desktop/build-linux.sh", "packaging/remote-desktop/checkout.sh",
                "packaging/remote-desktop/rustdesk-goim.bundle")
LOCK_PATH = "packaging/remote-desktop/release.lock.json"
PROTOCOL = 1


def run(*args):
    return subprocess.check_output(args, text=True).strip()


def gh_json(*args):
    return json.loads(run("gh", *args))


def digest_stream(stream):
    result = hashlib.sha256()
    for chunk in iter(lambda: stream.read(1024 * 1024), b""):
        result.update(chunk)
    return result.hexdigest()


def digest(path):
    with path.open("rb") as stream:
        return digest_stream(stream)


def read_revision(path, revision=None):
    if revision is None:
        return (ROOT / path).read_bytes()
    return subprocess.check_output(("git", "-C", str(ROOT), "show", f"{revision}:{path}"), stderr=subprocess.DEVNULL)


def recipe_digest(revision=None):
    # Triggers and publication do not affect compilation. Hash every compiler
    # job, target, feature, toolchain, dependency pin, source bundle and script.
    try:
        workflow = read_revision(BUILD_WORKFLOW, revision)
    except subprocess.CalledProcessError:
        workflow = read_revision(LEGACY_WORKFLOW, revision)
    workflow = workflow.replace(b"\r\n", b"\n")
    recipe = b"env:\n" + workflow.split(b"\nenv:\n", 1)[1]
    result = hashlib.sha256(recipe)
    for path in RECIPE_FILES:
        content = read_revision(path, revision)
        if not path.endswith(".bundle"):
            content = content.replace(b"\r\n", b"\n")
        result.update(path.encode() + b"\0" + content)
    return result.hexdigest()


def profile():
    version = (ROOT / "packaging/remote-desktop/version.txt").read_text().strip()
    if not re.fullmatch(r"(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)", version):
        raise ValueError("helper version must be a stable semantic version")
    commit = run("git", "bundle", "list-heads", str(ROOT / "packaging/remote-desktop/rustdesk-goim.bundle"),
                 "refs/heads/codex/goim-headless").split()[0]
    return {"schema": 1, "version": version, "tag": f"goim-rd-v{version}", "protocol": PROTOCOL,
            "source_commit": commit, "recipe_sha256": recipe_digest()}


def verify_manifest(manifest, target, expected, component=True):
    platform, arch = target.split("-")
    if (manifest["platform"], manifest["architecture"], manifest["protocol"], manifest["commit"]) != (
            platform, arch, expected["protocol"], expected["source_commit"]):
        raise ValueError(f"helper provenance mismatch for {target}")
    if manifest.get("source_archive") != "source.tar.gz":
        raise ValueError("helper must include the offline corresponding source")
    if component and (manifest["version"] != expected["version"] or
                      manifest.get("recipe_sha256") != expected["recipe_sha256"]):
        raise ValueError("helper version or build recipe does not match the pin")


def bundle_files(target):
    return {"goim-rd.exe" if target.startswith("windows-") else "goim-rd",
            "manifest.json", "LICENSE", "NOTICE", "source.tar.gz"}


def verify_bundle(directory, target, expected, component=True):
    manifest = json.loads((directory / "manifest.json").read_text(encoding="utf-8"))
    verify_manifest(manifest, target, expected, component)
    if {path.name for path in directory.iterdir()} != bundle_files(target):
        raise ValueError(f"unexpected files in {target} helper bundle")
    for path in directory.iterdir():
        if path.is_symlink() or not path.is_file():
            raise ValueError("helper bundle members must be regular files")
    binary = "goim-rd.exe" if target.startswith("windows-") else "goim-rd"
    if digest(directory / binary) != manifest["sha256"] or digest(directory / "source.tar.gz") != manifest["source_sha256"]:
        raise ValueError("helper binary or corresponding source checksum mismatch")
    if b"GNU AFFERO GENERAL PUBLIC LICENSE" not in (directory / "LICENSE").read_bytes():
        raise ValueError("helper license is missing")
    return manifest


def archive_bundle(directory, archive, target, expected, build_run):
    manifest = verify_bundle(directory, target, expected, component=False)
    manifest.update(version=expected["version"], recipe_sha256=expected["recipe_sha256"], build_run=str(build_run))
    manifest_bytes = (json.dumps(manifest, indent=2) + "\n").encode()
    binary = "goim-rd.exe" if target.startswith("windows-") else "goim-rd"
    with zipfile.ZipFile(archive, "w", compression=zipfile.ZIP_DEFLATED, compresslevel=6) as package:
        for name in sorted(bundle_files(target)):
            info = zipfile.ZipInfo(name)
            info.create_system = 3
            info.external_attr = (stat.S_IFREG | (0o755 if name == binary else 0o644)) << 16
            info.compress_type = zipfile.ZIP_DEFLATED
            with package.open(info, "w") as destination:
                if name == "manifest.json":
                    destination.write(manifest_bytes)
                else:
                    with (directory / name).open("rb") as source:
                        shutil.copyfileobj(source, destination)
    return hashlib.sha256(manifest_bytes).hexdigest()


def check_lock(lock, expected):
    if any(lock.get(key) != value for key, value in expected.items()):
        raise ValueError("helper pin is stale: publish the new helper recipe and update release.lock.json")
    if set(lock.get("assets", {})) != set(TARGETS):
        raise ValueError("helper release must pin all six platforms")
    for target, asset in lock["assets"].items():
        if asset["name"] != f"goim-rd_{expected['version']}_{target.replace('-', '_')}.zip":
            raise ValueError("invalid helper asset name")
        if any(not re.fullmatch(r"[0-9a-f]{64}", asset[key]) for key in ("sha256", "manifest_sha256")):
            raise ValueError("invalid helper checksum")


def unpack_bundle(archive, output, target, lock):
    asset = lock["assets"][target]
    if digest(archive) != asset["sha256"]:
        raise ValueError("helper archive checksum does not match the committed pin")
    with zipfile.ZipFile(archive) as package:
        if len(package.infolist()) != len(bundle_files(target)) or set(package.namelist()) != bundle_files(target):
            raise ValueError("invalid helper archive members")
        for info in package.infolist():
            limit = {"manifest.json": 64 * 1024, "LICENSE": 256 * 1024, "NOTICE": 256 * 1024,
                     "source.tar.gz": 512 * 1024 * 1024}.get(info.filename, 256 * 1024 * 1024)
            if info.file_size <= 0 or info.file_size > limit or stat.S_ISLNK(info.external_attr >> 16):
                raise ValueError("invalid helper archive member size or type")
        manifest = package.read("manifest.json")
        if hashlib.sha256(manifest).hexdigest() != asset["manifest_sha256"]:
            raise ValueError("helper manifest checksum does not match the committed pin")
        verify_manifest(json.loads(manifest), target, lock)
        output.mkdir(parents=True)
        for info in package.infolist():
            path = output / info.filename
            with package.open(info) as source, path.open("wb") as destination:
                shutil.copyfileobj(source, destination)
            path.chmod(0o755 if info.filename in ("goim-rd", "goim-rd.exe") else 0o644)
    verify_bundle(output, target, lock)


def verify_run(repository, run_id, expected):
    if not re.fullmatch(r"[0-9]+", str(run_id)):
        raise ValueError("invalid helper workflow run ID")
    build = gh_json("api", f"repos/{repository}/actions/runs/{run_id}")
    if (build["conclusion"] != "success" or build["path"] != LEGACY_WORKFLOW or
            build["event"] != "push" or build["head_repository"]["full_name"] != repository):
        raise ValueError("helper reuse requires a successful trusted helper push workflow")
    if recipe_digest(build["head_sha"]) != expected["recipe_sha256"]:
        raise ValueError("selected run used different helper sources or compiler settings")
    artifacts = gh_json("api", f"repos/{repository}/actions/runs/{run_id}/artifacts?per_page=100")["artifacts"]
    available = {item["name"] for item in artifacts if not item["expired"]}
    if not {f"goim-rd-{target}" for target in TARGETS} <= available:
        raise ValueError("selected run no longer has every helper artifact")


def verify_release(repository, expected):
    release = gh_json("release", "view", expected["tag"], "--repo", repository, "--json", "isDraft,assets")
    if release["isDraft"]:
        raise ValueError("helper component release is still a draft")
    with tempfile.TemporaryDirectory(prefix="goim-rd-pin-") as temporary:
        subprocess.run(("gh", "release", "download", expected["tag"], "--repo", repository,
                        "--pattern", "release.lock.json", "--dir", temporary), check=True)
        lock = json.loads((Path(temporary) / "release.lock.json").read_text(encoding="utf-8"))
    check_lock(lock, expected)
    uploaded = {asset["name"]: asset["digest"] for asset in release["assets"]}
    if any(uploaded.get(asset["name"]) != "sha256:" + asset["sha256"] for asset in lock["assets"].values()):
        raise ValueError("helper release asset checksums do not match the release pin")


def pack_all(source, output, build_run, expected):
    output.mkdir(parents=True, exist_ok=True)
    lock = dict(expected, build_run=str(build_run), assets={})
    for target in TARGETS:
        name = f"goim-rd_{expected['version']}_{target.replace('-', '_')}.zip"
        archive = output / name
        manifest_sha = archive_bundle(source / f"goim-rd-{target}", archive, target, expected, build_run)
        lock["assets"][target] = {"name": name, "sha256": digest(archive), "manifest_sha256": manifest_sha}
    check_lock(lock, expected)
    (output / "release.lock.json").write_text(json.dumps(lock, indent=2) + "\n", encoding="utf-8")
    sums = "".join(f"{digest(path)}  {path.name}\n" for path in sorted(output.iterdir())
                   if path.name.endswith(".zip") or path.name == "release.lock.json")
    (output / "SHA256SUMS").write_text(sums, encoding="utf-8")


def fetch_bundle(repository, target, output, expected):
    lock = json.loads((ROOT / LOCK_PATH).read_text(encoding="utf-8"))
    check_lock(lock, expected)
    output.parent.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(prefix="goim-rd-download-", dir=output.parent) as temporary:
        temporary = Path(temporary)
        name = lock["assets"][target]["name"]
        subprocess.run(("gh", "release", "download", lock["tag"], "--repo", repository,
                        "--pattern", name, "--dir", str(temporary)), check=True)
        unpack_bundle(temporary / name, output, target, lock)


def publish(repository, directory, revision, expected):
    lock = json.loads((directory / "release.lock.json").read_text(encoding="utf-8"))
    check_lock(lock, expected)
    tag = expected["tag"]
    result = subprocess.run(("gh", "release", "view", tag, "--repo", repository, "--json", "isDraft"),
                            text=True, capture_output=True)
    if result.returncode == 0 and not json.loads(result.stdout)["isDraft"]:
        raise ValueError("the helper version is already public; publish a new version")
    notes = directory / "RELEASE_NOTES.md"
    notes.write_text(
        f"# GoIM Remote Desktop Helper {expected['version']}\n\n"
        f"Source commit `{expected['source_commit']}`. Protocol `{expected['protocol']}`.\n\n"
        "The six platform bundles include the helper, pinned corresponding source, license, notice and build manifest.\n"
        "GoIM releases pin these archives and verify their SHA-256 before packaging.\n", encoding="utf-8")
    if result.returncode != 0:
        subprocess.run(("gh", "release", "create", tag, "--repo", repository, "--target", revision, "--draft",
                        "--latest=false", "--title", f"GoIM Remote Desktop Helper {expected['version']}",
                        "--notes-file", str(notes)), check=True)
    paths = [directory / asset["name"] for asset in lock["assets"].values()]
    paths += [directory / "release.lock.json", directory / "SHA256SUMS"]
    subprocess.run(("gh", "release", "upload", tag, "--repo", repository, "--clobber", *map(str, paths)), check=True)
    uploaded = gh_json("release", "view", tag, "--repo", repository, "--json", "assets")["assets"]
    actual = {asset["name"]: asset["digest"] for asset in uploaded}
    if any(actual.get(path.name) != "sha256:" + digest(path) for path in paths):
        raise ValueError("published helper asset checksum mismatch")
    subprocess.run(("gh", "release", "edit", tag, "--repo", repository, "--draft=false", "--latest=false"), check=True)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("command", choices=("profile", "verify-lock", "verify-run", "verify-release", "pack", "fetch", "publish"))
    parser.add_argument("--repository", default=os.environ.get("GITHUB_REPOSITORY", "Ezreal-byte/GoIM"))
    parser.add_argument("--run")
    parser.add_argument("--input", type=Path)
    parser.add_argument("--output", type=Path)
    parser.add_argument("--target", choices=TARGETS)
    parser.add_argument("--revision", default=os.environ.get("GITHUB_SHA", "HEAD"))
    args = parser.parse_args()
    expected = profile()
    if args.command == "profile":
        print(json.dumps(expected))
    elif args.command == "verify-lock":
        check_lock(json.loads((ROOT / LOCK_PATH).read_text(encoding="utf-8")), expected)
    elif args.command == "verify-run":
        verify_run(args.repository, args.run, expected)
    elif args.command == "verify-release":
        verify_release(args.repository, expected)
    elif args.command == "pack":
        pack_all(args.input, args.output, args.run, expected)
    elif args.command == "fetch":
        fetch_bundle(args.repository, args.target, args.output, expected)
    elif args.command == "publish":
        publish(args.repository, args.input, args.revision, expected)


if __name__ == "__main__":
    main()