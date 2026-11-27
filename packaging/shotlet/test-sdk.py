"""Exercise the installed Shotlet host protocol on an isolated 1280x800 Xvfb desktop."""
import argparse
import importlib.util
import os
from pathlib import Path
import struct
import subprocess
import tempfile
import time

from PIL import Image, ImageGrab


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--helper", type=Path, required=True)
    parser.add_argument("--source", type=Path, required=True)
    parser.add_argument("--artifacts", type=Path, required=True)
    parser.add_argument("--dialog-bin", type=Path,
                        help="Private file-dialog bin directory; defaults to the installed sibling runtime")
    args = parser.parse_args()
    args.artifacts.mkdir(parents=True, exist_ok=True)
    helper = args.helper.resolve()
    dialog_bin = (args.dialog_bin.resolve() if args.dialog_bin else
                  helper.parent.parent / "file-dialog" / "bin")
    if not (dialog_bin / "zenity").is_file():
        parser.error(f"Packaged file-dialog runtime is missing: {dialog_bin}")
    # Reuse the desktop fixture that belongs to this immutable upstream revision.
    spec = importlib.util.spec_from_file_location("shotlet_gui", args.source / "tests/test_linux_gui.py")
    gui = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(gui)
    background = gui.Background()
    process = None
    with tempfile.TemporaryDirectory(prefix="goim-shotlet-sdk-") as temporary:
        env = dict(os.environ, XDG_SESSION_TYPE="x11", WAYLAND_DISPLAY="", LC_ALL="C.UTF-8",
                   XDG_CONFIG_HOME=f"{temporary}/config", XDG_STATE_HOME=f"{temporary}/state",
                   XDG_CACHE_HOME=f"{temporary}/cache", SDL_FILE_DIALOG_DRIVER="zenity",
                   PATH=str(dialog_bin) + os.pathsep + os.environ.get("PATH", ""))
        # GoIM's bundle.Configure supplies this PATH entry before spawning Shotlet.
        # Exercise the packaged chooser even when the runner has a system Zenity.
        try:
            def start():
                nonlocal process
                process = subprocess.Popen([str(helper), "--language", "zh-CN",
                                            "--theme", "dark", "--log-dir", f"{temporary}/logs"],
                                           env=env, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
                window = gui.wait_for(lambda: gui.find_window("^Shotlet_"))
                gui.run("xdotool", "windowfocus", window)
                time.sleep(.5)

            start()
            gui.drag(200, 150, 1000, 600)
            gui.wait_for(lambda: min(ImageGrab.grab().convert("RGB").getpixel((319, 633))) > 80)
            gui.click(274, 642)
            gui.wait_selected_tool(274)
            gui.drag(300, 230, 550, 430)
            gui.click(589, 642)
            gui.wait_selected_tool(589)
            gui.click(680, 280)
            gui.run("xdotool", "type", "--clearmodifiers", "--delay", "80", "麒麟 UOS 标注")
            time.sleep(.4)
            ImageGrab.grab().save(args.artifacts / "sdk-annotation.png")
            gui.click(924, 642)
            output, error = process.communicate(timeout=20)
            assert process.returncode == 0, (process.returncode, error)
            assert output[:8] == b"ZMSHOT1\n", output[:64]
            width, height = struct.unpack("<II", output[8:16])
            assert (width, height) == (800, 450), (width, height)
            assert len(output) == 16 + width * height * 4, len(output)
            picture = Image.frombytes("RGBA", (width, height), output[16:])
            assert picture.getpixel((10, 10)) == (0x28, 0x50, 0x70, 255)
            assert picture.getpixel((420, 330)) == (0xf1, 0xc7, 0x5b, 255)
            red = lambda p: p[0] > 180 and p[1] < 120 and p[2] < 120
            assert red(picture.getpixel((100, 140))), "rectangle missing from host output"
            assert sum(red(picture.getpixel((x, y))) for y in range(120, 170) for x in range(478, 508)) > 30, "Chinese annotation missing"
            picture.save(args.artifacts / "sdk-result.png")

            start()
            gui.run("xdotool", "key", "Escape")
            output, error = process.communicate(timeout=15)
            assert process.returncode == 2 and not output, (process.returncode, error)

            start()
            gui.drag(200, 150, 1000, 600)
            gui.run("xdotool", "key", "ctrl+s")
            dialog = gui.wait_for(lambda: gui.find_window("保存截图"))
            gui.run("xdotool", "windowfocus", "--sync", dialog)
            path = args.artifacts.resolve() / f"saved-{time.time_ns()}.png"
            gui.run("xdotool", "key", "ctrl+l")
            # GTK 3 selects the basename without its suffix in the save entry.
            gui.run("xdotool", "key", "ctrl+a")
            gui.run("xdotool", "type", "--clearmodifiers", "--delay", "5", str(path))
            ImageGrab.grab().save(args.artifacts / "save-dialog.png")
            gui.run("xdotool", "key", "Return")
            time.sleep(.5)
            if not path.exists():
                gui.run("xdotool", "key", "Return")
            gui.wait_for(path.exists)
            output, error = process.communicate(timeout=15)
            assert process.returncode == 4 and not output, (process.returncode, error)
            assert Image.open(path).size == (800, 450)
            print("PASS: packaged host capture, annotation, Chinese text, cancel and save-only status")
        finally:
            if process and process.poll() is None:
                ImageGrab.grab().save(args.artifacts / "failure.png")
                process.kill()
                _, error = process.communicate(timeout=10)
                (args.artifacts / "failure-stderr.log").write_bytes(error)
            background.close()


if __name__ == "__main__":
    main()
