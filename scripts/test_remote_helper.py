import copy
import hashlib
import importlib.util
import json
from pathlib import Path
import tempfile
import unittest
import zipfile

spec = importlib.util.spec_from_file_location("remote_helper", Path(__file__).with_name("remote-helper.py"))
helper = importlib.util.module_from_spec(spec)
spec.loader.exec_module(helper)


class RemoteHelperTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        self.expected = {"schema": 1, "version": "0.1.0", "tag": "goim-rd-v0.1.0", "protocol": 1,
                         "source_commit": "a" * 40, "recipe_sha256": "b" * 64}
        source = self.root / "source"
        for target in helper.TARGETS:
            directory = source / f"goim-rd-{target}"
            directory.mkdir(parents=True)
            binary = directory / ("goim-rd.exe" if target.startswith("windows-") else "goim-rd")
            binary.write_bytes(b"native executable fixture")
            (directory / "LICENSE").write_text("GNU AFFERO GENERAL PUBLIC LICENSE", encoding="utf-8")
            (directory / "NOTICE").write_text("fixture notice", encoding="utf-8")
            (directory / "source.tar.gz").write_bytes(b"corresponding source fixture")
            platform, arch = target.split("-")
            manifest = {"version": "1.5.3-goim.fixture", "protocol": 1, "platform": platform,
                        "architecture": arch, "commit": self.expected["source_commit"],
                        "sha256": helper.digest(binary), "source_archive": "source.tar.gz",
                        "source_sha256": helper.digest(directory / "source.tar.gz")}
            (directory / "manifest.json").write_text(json.dumps(manifest), encoding="utf-8")
        self.source = source
        self.output = self.root / "release"
        helper.pack_all(source, self.output, "123", self.expected)
        self.lock = json.loads((self.output / "release.lock.json").read_text())
        self.target = "windows-amd64"
        self.archive = self.output / self.lock["assets"][self.target]["name"]

    def test_component_reuses_binary_and_source_with_independent_version(self):
        output = self.root / "consumer"
        helper.unpack_bundle(self.archive, output, self.target, self.lock)
        manifest = json.loads((output / "manifest.json").read_text())
        self.assertEqual(manifest["version"], "0.1.0")
        self.assertEqual(manifest["build_run"], "123")
        self.assertEqual((output / "goim-rd.exe").read_bytes(), b"native executable fixture")
        self.assertEqual((output / "source.tar.gz").read_bytes(), b"corresponding source fixture")
        self.assertEqual(set(self.lock["assets"]), set(helper.TARGETS))

    def test_source_recipe_version_and_protocol_changes_require_a_new_pin(self):
        for key, value in (("recipe_sha256", "c" * 64), ("source_commit", "c" * 40),
                           ("version", "0.1.1"), ("protocol", 2)):
            with self.subTest(key=key):
                changed = dict(self.expected, **{key: value})
                with self.assertRaises(ValueError):
                    helper.check_lock(self.lock, changed)

    def test_corrupt_archive_is_rejected_before_extraction(self):
        with self.archive.open("ab") as stream:
            stream.write(b"corruption")
        output = self.root / "consumer"
        with self.assertRaisesRegex(ValueError, "archive checksum"):
            helper.unpack_bundle(self.archive, output, self.target, self.lock)
        self.assertFalse(output.exists())

    def test_archive_cannot_write_outside_the_destination(self):
        with zipfile.ZipFile(self.archive, "a") as package:
            package.writestr("../outside", "unexpected")
        self.lock["assets"][self.target]["sha256"] = helper.digest(self.archive)
        with self.assertRaisesRegex(ValueError, "archive members"):
            helper.unpack_bundle(self.archive, self.root / "consumer", self.target, self.lock)
        self.assertFalse((self.root / "outside").exists())

    def test_wrong_architecture_and_manifest_digest_are_rejected(self):
        for alter in ("target", "checksum"):
            with self.subTest(alter=alter):
                lock = copy.deepcopy(self.lock)
                if alter == "checksum":
                    lock["assets"][self.target]["manifest_sha256"] = "0" * 64
                else:
                    manifest = json.loads(zipfile.ZipFile(self.archive).read("manifest.json"))
                    manifest["architecture"] = "arm64"
                    data = json.dumps(manifest).encode()
                    replacement = self.root / "wrong.zip"
                    with zipfile.ZipFile(self.archive) as old, zipfile.ZipFile(replacement, "w") as new:
                        for name in old.namelist():
                            new.writestr(name, data if name == "manifest.json" else old.read(name))
                    lock["assets"][self.target]["sha256"] = helper.digest(replacement)
                    lock["assets"][self.target]["manifest_sha256"] = hashlib.sha256(data).hexdigest()
                    with self.assertRaises(ValueError):
                        helper.unpack_bundle(replacement, self.root / "wrong-target", self.target, lock)
                    continue
                with self.assertRaises(ValueError):
                    helper.unpack_bundle(self.archive, self.root / "wrong-manifest", self.target, lock)

    def test_binary_and_source_checksums_are_required_when_promoting_a_build(self):
        directory = self.source / f"goim-rd-{self.target}"
        for filename in ("goim-rd.exe", "source.tar.gz"):
            with self.subTest(filename=filename):
                original = (directory / filename).read_bytes()
                (directory / filename).write_bytes(b"changed")
                with self.assertRaisesRegex(ValueError, "checksum mismatch"):
                    helper.archive_bundle(directory, self.root / "bad.zip", self.target, self.expected, "123")
                (directory / filename).write_bytes(original)


if __name__ == "__main__":
    unittest.main()