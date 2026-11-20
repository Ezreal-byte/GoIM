"""Drive the verified FeiQ process on a disposable CI Windows desktop."""
import hashlib
import json
import os
import sys
import time
from pathlib import Path

from pywinauto import Desktop, keyboard
import win32clipboard
import struct
import win32con
import win32gui
import win32process

pid, root = int(sys.argv[1]), Path(sys.argv[2]).resolve()
if os.getenv("RUNNER_ENVIRONMENT") != "github-hosted" or os.getenv("GOIM_FEIQ_LAB") != "1":
    raise SystemExit("Requires the disposable FeiQ runner")
desktop = Desktop(backend="uia")
flags = {"outgoing_text_visible": False, "outgoing_file_sha256": False}
seen_windows, seen_controls = set(), set()
errors = set()
deadline = time.monotonic() + 95
plan = None
for _ in range(80):
    try:
        plan = json.loads((root / "plan.json").read_text(encoding="utf-8"))
        break
    except (OSError, ValueError):
        time.sleep(0.1)
if not plan:
    raise SystemExit("GoIM did not produce the test plan")


def path_hash(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def send_clipboard(target, text=None, file=None):
    target.set_focus()
    win32clipboard.OpenClipboard()
    try:
        win32clipboard.EmptyClipboard()
        if file is not None:
            # Standard Explorer file-list paste. The original client owns the offer.
            drop = struct.pack("<IiiII", 20, 0, 0, 0, 1)
            win32clipboard.SetClipboardData(win32con.CF_HDROP, drop + (file + "\0\0").encode("utf-16le"))
        else:
            win32clipboard.SetClipboardText(text, win32con.CF_UNICODETEXT)
    finally:
        win32clipboard.CloseClipboard()
    keyboard.send_keys("^v")
    time.sleep(0.25)
    keyboard.send_keys("{ENTER}")


def ui_text(control):
    parts = [control.window_text()]
    try:
        parts.append(control.iface_text.DocumentRange.GetText(-1))
    except Exception:
        pass
    try:
        parts.append(control.iface_value.CurrentValue)
    except Exception:
        pass
    return "\n".join(p for p in parts if p)


sent_text, sent_file = False, False
opened_peer_at = accepted_at = 0
try:
    while time.monotonic() < deadline:
        if not desktop.windows(process=pid, visible_only=True):
            def show_original(hwnd, _):
                if win32process.GetWindowThreadProcessId(hwnd)[1] != pid:
                    return
                left, top, right, bottom = win32gui.GetWindowRect(hwnd)
                if right - left >= 180 and bottom - top >= 90 and win32gui.GetWindowText(hwnd):
                    win32gui.ShowWindow(hwnd, win32con.SW_SHOWNOACTIVATE)
            win32gui.EnumWindows(show_original, None)
        for w in desktop.windows(process=pid, visible_only=True):
            try:
                title = w.window_text()
                seen_windows.add(title[:160])
                controls = w.descendants()
                content = []
                for c in controls:
                    label = c.window_text()
                    kind = c.element_info.control_type
                    if label and len(seen_controls) < 180:
                        seen_controls.add((kind, label[:85]))
                    content.append(ui_text(c) if kind in ("Document", "Edit", "Text", "Pane") else label)
                body = "\n".join(content)
                if plan["OutgoingText"] in body:
                    flags["outgoing_text_visible"] = True
                # A stock save dialog belongs to FeiQ but does not show the chat title.
                if any(x in title.lower() for x in ("另存为", "保存", "save", "接收文件")):
                    edits = [c for c in controls if c.element_info.control_type == "Edit"]
                    buttons = [c for c in controls if c.element_info.control_type == "Button"
                               and any(x in c.window_text().lower() for x in ("保存", "save"))]
                    if edits and buttons and (accepted_at or Path(plan["OutgoingFile"]).name in body):
                        target = root / "stock-received" / Path(plan["OutgoingFile"]).name
                        edits[-1].set_edit_text(str(target))
                        buttons[0].invoke()
                        continue
                # Resolve a normal first-run or incoming message prompt. Only interact with FeiQ.
                if not any(x in (title + body) for x in (plan["Name"], plan["OutgoingText"])):
                    for c in controls:
                        if c.element_info.control_type == "Button" and c.window_text().strip() in ("完成", "确定", "OK", "进入飞秋"):
                            c.invoke()
                            break
                ref = plan["Name"] in title or plan["OutgoingText"] in body
                if not ref:
                    for c in controls:
                        if (c.element_info.control_type in ("TreeItem", "ListItem", "DataItem") and
                                plan["Name"] in c.window_text() and time.monotonic() - opened_peer_at > 3):
                            opened_peer_at = time.monotonic()
                            c.double_click_input()
                            break
                    continue
                edits = [c for c in controls if c.element_info.control_type in ("Edit", "Document") and c.is_enabled()]
                for edit in reversed(edits):
                    try:
                        if edit.iface_value.CurrentIsReadOnly:
                            continue
                    except Exception:
                        pass
                    if not sent_text:
                        send_clipboard(edit, text=plan["IncomingText"])
                        sent_text = True
                    elif not sent_file:
                        send_clipboard(edit, file=plan["IncomingFile"])
                        sent_file = True
                    break
                if time.monotonic() - accepted_at > 2:
                    for c in controls:
                        if c.element_info.control_type in ("Button", "Hyperlink", "Text") and c.window_text().strip() in ("接收", "接受", "全部接收", "接收文件"):
                            c.click_input()
                            accepted_at = time.monotonic()
                            break
            except Exception as ex:
                if len(errors) < 20:
                    errors.add(type(ex).__name__ + ": " + str(ex)[:130])
        # The checker owns a separate directory. Only original-client save locations count.
        name = Path(plan["OutgoingFile"]).name
        for base in (root / "original", Path.home() / "Documents", Path.home() / "Downloads",
                     Path(os.environ["APPDATA"]) / "FeiQ", root / "stock-received"):
            if not base.is_dir():
                continue
            for p in base.rglob(name):
                if p.is_file() and p.stat().st_size == 98321 and path_hash(p) == plan["OutgoingHash"]:
                    flags["outgoing_file_sha256"] = True
        if all(flags.values()) and sent_text and sent_file:
            break
        time.sleep(0.6)
finally:
    (root / "ui.json").write_text(json.dumps({"checks": flags, "attempted_reply": sent_text,
        "attempted_file_offer": sent_file, "original_window_titles": sorted(seen_windows),
        "controls": sorted(seen_controls), "driver_errors": sorted(errors)},
        ensure_ascii=False, indent=2), encoding="utf-8")
    (root / "ui-finished").touch()
