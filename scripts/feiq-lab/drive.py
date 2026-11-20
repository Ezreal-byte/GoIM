"""Drive the verified FeiQ process on a disposable CI Windows desktop."""
import hashlib
import json
import os
import struct
import sys
import time
from pathlib import Path

from PIL import ImageGrab
from pywinauto import Desktop, keyboard
import win32clipboard
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


# The verified 2013 binary uses Program Files/feiq/Recv Files on a new profile.
# A named Save As operation uses our separate stock-received folder instead.
receive_dirs = {root / "stock-received"}
for key in ("ProgramFiles", "ProgramFiles(x86)", "ProgramW6432"):
    if os.getenv(key):
        receive_dirs.add(Path(os.environ[key]) / "feiq" / "Recv Files")
receive_paths = [directory / Path(plan["OutgoingFile"]).name for directory in receive_dirs]


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
    if text is not None:
        keyboard.send_keys("%s")


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


def capture_chat(w, controls, stage):
    image_path = root / ("chat-" + stage + ".png")
    if image_path.exists():
        return
    try:
        w.set_focus()
        time.sleep(0.2)
        area = w.rectangle()
        ImageGrab.grab(bbox=(area.left, area.top, area.right, area.bottom)).save(image_path)
        detail = []
        for c in controls:
            r = c.rectangle()
            detail.append({"kind": c.element_info.control_type, "class": c.class_name(),
                           "id": c.element_info.automation_id, "name": c.window_text()[:60],
                           "rect": [r.left-area.left, r.top-area.top, r.right-area.left, r.bottom-area.top]})
        (root / "chat-controls.json").write_text(json.dumps(detail, ensure_ascii=False, indent=1), encoding="utf-8")
    except Exception as ex:
        errors.add(type(ex).__name__ + ": " + str(ex)[:130])


sent_text, sent_file = False, False
opened_peer_at = accepted_at = file_submit_at = 0
saved_path = chat_log = ""
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
                for c in controls:
                    if c.element_info.automation_id == "1151":
                        chat_log = ui_text(c)[-6000:]
                stage = "file" if sent_file else "text" if sent_text else "open"
                capture_chat(w, controls, stage)
                edits = [c for c in controls if c.element_info.control_type in ("Edit", "Document") and c.is_enabled()]
                edits.sort(key=lambda c: c.rectangle().top)
                for edit in reversed(edits):
                    try:
                        if edit.iface_value.CurrentIsReadOnly:
                            continue
                    except Exception:
                        pass
                    handle = edit.element_info.handle
                    if handle and "rich" in win32gui.GetClassName(handle).lower() and win32gui.GetWindowLong(handle, win32con.GWL_STYLE) & 0x800:
                        continue
                    if not sent_text:
                        send_clipboard(edit, text=plan["IncomingText"])
                        sent_text = True
                    elif not sent_file:
                        send_clipboard(edit, file=plan["IncomingFile"])
                        sent_file = True
                    break
                # These clickable labels in FeiQ 3.0.0.2 are owner-drawn. Position them
                # relative to the adjacent accessible lists and editor in the stock dialog.
                receive = next((c for c in controls if c.element_info.automation_id == "1034" and c.class_name() == "SysListView32"), None)
                composer = next((c for c in edits if c.element_info.automation_id == "1152"), None)
                if receive is not None and Path(plan["OutgoingFile"]).name in body and time.monotonic() - accepted_at > 12:
                    area, listing = w.rectangle(), receive.rectangle()
                    w.click_input(coords=(listing.right - area.left - 61, listing.top - area.top - 30))
                    accepted_at = time.monotonic()
                    time.sleep(0.3)
                if composer is not None and Path(plan["IncomingFile"]).name in body and time.monotonic() - file_submit_at > 12:
                    area, entry = w.rectangle(), composer.rectangle()
                    w.click_input(coords=(entry.right - area.left - 46, entry.bottom - area.top + 14))
                    file_submit_at = time.monotonic()
                    time.sleep(0.3)
                    capture_chat(w, controls, "submitted")
                if time.monotonic() - accepted_at > 2:
                    for c in controls:
                        if c.element_info.control_type in ("Button", "Hyperlink", "Text") and c.window_text().strip() in ("接收", "接受", "全部接收", "接收文件"):
                            c.click_input()
                            accepted_at = time.monotonic()
                            break
            except Exception as ex:
                if len(errors) < 20:
                    errors.add(type(ex).__name__ + ": " + str(ex)[:130])
        for p in receive_paths:
            try:
                if accepted_at and p.is_file() and p.stat().st_size == 98321 and path_hash(p) == plan["OutgoingHash"]:
                    flags["outgoing_file_sha256"] = True
                    saved_path = str(p)
                    break
            except OSError:
                continue
        if all(flags.values()) and (root / "backend-complete").exists():
            break
        time.sleep(0.6)
finally:
    (root / "ui.json").write_text(json.dumps({"checks": flags, "attempted_reply": sent_text,
        "attempted_file_offer": sent_file, "original_window_titles": sorted(seen_windows),
        "controls": sorted(seen_controls), "driver_errors": sorted(errors), "original_saved_path": saved_path,
        "original_chat_transcript": chat_log},
        ensure_ascii=False, indent=2), encoding="utf-8")
    (root / "ui-finished").touch()
