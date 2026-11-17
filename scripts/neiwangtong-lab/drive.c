// Test fixture for the original 3.4.3055 client in a private Wine display.
// Its skin paints custom widgets. Use the stock layout in a fresh profile
// and require the expected process, window title and dimensions.
#include <windows.h>
#include <commctrl.h>
#include <stdio.h>
#include <wchar.h>

struct window_query {
    const wchar_t *exe, *title, *class_name;
    HWND result;
};

static BOOL CALLBACK match_window(HWND window, LPARAM arg) {
    struct window_query *query = (struct window_query *)arg;
    wchar_t title[128], class_name[32], exe[MAX_PATH];
    if (!IsWindowVisible(window)) return TRUE;
    if (!GetWindowTextW(window, title, _countof(title)) ||
        !GetClassNameW(window, class_name, _countof(class_name))) return TRUE;
    if (wcscmp(title, query->title) || wcscmp(class_name, query->class_name)) return TRUE;
    DWORD pid, size = _countof(exe);
    if (!GetWindowThreadProcessId(window, &pid)) return TRUE;
    HANDLE process = OpenProcess(PROCESS_QUERY_LIMITED_INFORMATION, FALSE, pid);
    if (!process) return TRUE;
    BOOL match = QueryFullProcessImageNameW(process, 0, exe, &size) && !_wcsicmp(exe, query->exe);
    CloseHandle(process);
    if (!match) return TRUE;
    query->result = window;
    return FALSE;
}

static HWND wait_for_window(const wchar_t *exe, const wchar_t *title, const wchar_t *class_name) {
    struct window_query query = {exe, title, class_name, NULL};
    for (int i = 0; i < 120; ++i) {
        EnumWindows(match_window, (LPARAM)&query);
        if (query.result) return query.result;
        Sleep(50);
    }
    return NULL;
}

static BOOL CALLBACK describe_window(HWND window, LPARAM arg) {
    const wchar_t *exe = (const wchar_t *)arg;
    wchar_t path[MAX_PATH], name[128], cls[48];
    DWORD pid, size = _countof(path);
    if (!IsWindowVisible(window) || !GetWindowThreadProcessId(window, &pid)) return TRUE;
    HANDLE process = OpenProcess(PROCESS_QUERY_LIMITED_INFORMATION, FALSE, pid);
    if (!process) return TRUE;
    BOOL match = QueryFullProcessImageNameW(process, 0, path, &size) && !_wcsicmp(path, exe);
    CloseHandle(process);
    if (match && GetWindowTextW(window, name, _countof(name)) && GetClassNameW(window, cls, _countof(cls))) {
        char title[384], class_name[144];
        RECT rect = {0};
        GetClientRect(window, &rect);
        if (WideCharToMultiByte(CP_UTF8, 0, name, -1, title, _countof(title), NULL, NULL) &&
            WideCharToMultiByte(CP_UTF8, 0, cls, -1, class_name, _countof(class_name), NULL, NULL))
            fprintf(stderr, "%s: %s (%ldx%ld)\n", class_name, title, rect.right, rect.bottom);
    }
    return TRUE;
}

static BOOL activate_control(HWND window, int x, int y);
static int fail(const char *message);

static void dismiss_guide(const wchar_t *exe) {
    struct window_query guide = {exe, L"新手引导", L"SkinDialog", NULL};
    EnumWindows(match_window, (LPARAM)&guide);
    if (guide.result) {
        PostMessageW(guide.result, WM_CLOSE, 0, 0);
        Sleep(350);
        RECT r;
        if (IsWindowVisible(guide.result) && GetClientRect(guide.result, &r) && r.right == 744 && r.bottom == 559) {
            activate_control(guide.result, r.right - 27, 27);
            Sleep(250);
        }
    }
}

static BOOL focus_chat(HWND chat) {
    MSG unused;
    PeekMessageW(&unused, NULL, 0, 0, PM_NOREMOVE);
    DWORD current = GetCurrentThreadId(), thread = GetWindowThreadProcessId(chat, NULL);
    if (!thread || !AttachThreadInput(current, thread, TRUE)) return FALSE;
    BringWindowToTop(chat);
    SetForegroundWindow(chat);
    SetActiveWindow(chat);
    SetFocus(chat);
    BOOL active = GetForegroundWindow() == chat;
    AttachThreadInput(current, thread, FALSE);
    return active;
}

static BOOL activate_control(HWND window, int x, int y) {
    LPARAM point = MAKELPARAM(x, y);
    return PostMessageW(window, WM_MOUSEMOVE, 0, point)
        && PostMessageW(window, WM_LBUTTONDOWN, MK_LBUTTON, point)
        && PostMessageW(window, WM_LBUTTONUP, 0, point);
}

static BOOL enter_fixture_text(HWND window, const wchar_t *text) {
    for (const wchar_t *p = text; *p; ++p) {
        DWORD_PTR result;
        if (!SendMessageTimeoutW(window, WM_CHAR, *p, 1, SMTO_ABORTIFHUNG, 2000, &result))
            return FALSE;
    }
    return TRUE;
}

static BOOL chat_shortcut(HWND chat, WORD key) {
    if (GetForegroundWindow() != chat) return FALSE;
    INPUT input[4] = {0};
    for (int i = 0; i < 4; ++i) input[i].type = INPUT_KEYBOARD;
    input[0].ki.wVk = input[3].ki.wVk = VK_CONTROL;
    input[1].ki.wVk = input[2].ki.wVk = key;
    input[2].ki.dwFlags = input[3].ki.dwFlags = KEYEVENTF_KEYUP;
    return SendInput(_countof(input), input, sizeof(input[0])) == _countof(input);
}

static BOOL copied_transcript_matches(HWND chat, int y, const wchar_t *present, const wchar_t *absent) {
    DWORD sequence = GetClipboardSequenceNumber();
    if (!activate_control(chat, 254, y)) return FALSE;
    Sleep(150);
    if (!chat_shortcut(chat, 'A')) return FALSE;
    Sleep(100);
    if (!chat_shortcut(chat, 'C')) return FALSE;
    for (int i = 0; i < 24; ++i) {
        if (GetClipboardSequenceNumber() != sequence && OpenClipboard(NULL)) {
            HGLOBAL handle = GetClipboardData(CF_UNICODETEXT);
            const wchar_t *contents = handle ? GlobalLock(handle) : NULL;
            BOOL matched = contents && wcsstr(contents, present) && (!absent || !wcsstr(contents, absent));
            if (contents) GlobalUnlock(handle);
            CloseClipboard();
            return matched;
        }
        Sleep(50);
    }
    return FALSE;
}

static BOOL copied_transcript_contains(HWND chat, int y, const wchar_t *marker) {
    return copied_transcript_matches(chat, y, marker, NULL);
}

static BOOL click_fixture_screen(POINT point, HWND window, BOOL right) {
    if (GetAncestor(WindowFromPoint(point), GA_ROOT) != window || !SetCursorPos(point.x, point.y)) return FALSE;
    Sleep(160);
    INPUT click[2] = {0};
    click[0].type = click[1].type = INPUT_MOUSE;
    click[0].mi.dwFlags = right ? MOUSEEVENTF_RIGHTDOWN : MOUSEEVENTF_LEFTDOWN;
    click[1].mi.dwFlags = right ? MOUSEEVENTF_RIGHTUP : MOUSEEVENTF_LEFTUP;
    return SendInput(2, click, sizeof(click[0])) == 2;
}

static int recall_fixture_message(const wchar_t *exe, HWND chat, const wchar_t *marker, BOOL group) {
    dismiss_guide(exe);
    ShowWindow(chat, SW_RESTORE);
    if (!SetWindowPos(chat, HWND_TOPMOST, 180, 82, 0, 0, SWP_NOSIZE | SWP_SHOWWINDOW) || !focus_chat(chat))
        return fail("could not activate the private recall fixture");
    Sleep(550);
    if (!copied_transcript_contains(chat, 220, marker) && !copied_transcript_contains(chat, 135, marker) &&
        !copied_transcript_contains(chat, 320, marker)) return fail("the original's transcript did not contain the recall fixture");

    // This conversation contains just one message sent by the original. The
    // transcript can insert a timestamp before it and shift its position.
    const int direct_rows[] = {283, 307, 330, 277, 301};
    const int group_rows[] = {342, 366, 390, 336, 359};
    const int *rows = group ? group_rows : direct_rows;
    DWORD expected = 0;
    GetWindowThreadProcessId(chat, &expected);
    for (unsigned i = 0; i < _countof(direct_rows); ++i) {
        POINT bubble = {759, rows[i]};
        if (!click_fixture_screen(bubble, chat, TRUE)) continue;
        Sleep(320);
        POINT inside = {bubble.x + 36, bubble.y + 22};
        HWND menu = GetAncestor(WindowFromPoint(inside), GA_ROOT);
        DWORD actual = 0;
        RECT bounds;
        wchar_t cls[32];
        if (!menu || menu == chat || !GetWindowThreadProcessId(menu, &actual) || expected != actual ||
            !GetClassNameW(menu, cls, _countof(cls)) || wcscmp(cls, L"SkinDialog") ||
            !GetWindowRect(menu, &bounds) || bounds.right - bounds.left != 174) continue;
        if (bounds.bottom - bounds.top == 188) {
            POINT row = {bounds.left + 42, bounds.top + 132};
            return click_fixture_screen(row, menu, FALSE) ? 0 : fail("could not select recall for the stock fixture");
        }
        PostMessageW(menu, WM_CLOSE, 0, 0);
        Sleep(170);
    }
    return fail("expected the 3.4.3055 message popup with an enabled recall action");
}

static int exit_fixture_group(const wchar_t *exe, HWND chat) {
    dismiss_guide(exe);
    ShowWindow(chat, SW_RESTORE);
    if (!SetWindowPos(chat, HWND_TOPMOST, 180, 82, 0, 0, SWP_NOSIZE | SWP_SHOWWINDOW) || !focus_chat(chat))
        return fail("could not activate the private stock group");
    Sleep(550);
    POINT control = {1117, 142};
    if (!click_fixture_screen(control, chat, FALSE)) return fail("the private group exit control is not visible");
    Sleep(400);
    SetWindowPos(chat, HWND_NOTOPMOST, 0, 0, 0, 0, SWP_NOACTIVATE | SWP_NOMOVE | SWP_NOSIZE);
    HWND confirm = wait_for_window(exe, L"内网通", L"SkinDialog");
    RECT r;
    if (!confirm || IsWindowEnabled(chat) || !GetWindowRect(confirm, &r) ||
        r.right - r.left != 224 || r.bottom - r.top != 160)
        return fail("expected the stock group exit confirmation");
    POINT accept = {r.left + 61, r.top + 115};
    return click_fixture_screen(accept, confirm, FALSE) ? 0 : fail("could not confirm the fixture group exit");
}

static BOOL CALLBACK match_filename_edit(HWND window, LPARAM arg) {
    wchar_t class_name[32];
    if (GetDlgCtrlID(window) != 1148 || !IsWindowVisible(window) ||
        !GetClassNameW(window, class_name, _countof(class_name)) || wcscmp(class_name, L"Edit"))
        return TRUE;
    *(HWND *)arg = window;
    return FALSE;
}

static BOOL CALLBACK match_folder_tree(HWND window, LPARAM arg) {
    wchar_t class_name[32];
    if (!IsWindowVisible(window) || !GetClassNameW(window, class_name, _countof(class_name)))
        return TRUE;
    if (wcscmp(class_name, WC_TREEVIEWW)) return TRUE;
    *(HWND *)arg = window;
    return FALSE;
}

static int fail(const char *message) {
    fputs(message, stderr);
    fputc('\n', stderr);
    return 1;
}

struct file_picker {
    DWORD pid;
    HWND window, edit;
};

static BOOL CALLBACK match_profile_picker(HWND window, LPARAM param) {
    struct file_picker *picker = (struct file_picker *)param;
    DWORD pid = 0;
    wchar_t cls[32];
    if (!IsWindowVisible(window) || !GetWindowThreadProcessId(window, &pid) || pid != picker->pid ||
        !GetClassNameW(window, cls, _countof(cls)) || wcscmp(cls, L"#32770")) return TRUE;
    EnumChildWindows(window, match_filename_edit, (LPARAM)&picker->edit);
    if (!picker->edit) return TRUE;
    picker->window = window;
    return FALSE;
}

static int stock_avatar(const wchar_t *exe, const wchar_t *path) {
    BOOL clear = !wcscmp(path, L"default");
    if (!clear && (wcscmp(path, L"C:\\goim-qa\\profile-red.png") && wcscmp(path, L"C:\\goim-qa\\profile-green.png")))
        return fail("unexpected stock avatar filename");
    if (!clear && GetFileAttributesW(path) == INVALID_FILE_ATTRIBUTES)
        return fail("stock avatar fixture does not exist");
    dismiss_guide(exe);
    HWND main = wait_for_window(exe, L"单位名称", L"SkinDialog");
    RECT size;
    if (!main || !GetClientRect(main, &size) || size.right != 304 || size.bottom != 574 || !activate_control(main, 57, 98))
        return fail("could not open the original self profile");
    HWND profile = wait_for_window(exe, L"个人信息", L"SkinDialog");
    if (!profile || !GetClientRect(profile, &size) || size.right != 324 || size.bottom != 524 ||
        !focus_chat(profile) || !activate_control(profile, 266, 124))
        return fail("could not edit the original self profile");
    Sleep(250);
    if (!activate_control(profile, 163, 82)) return fail("could not open the original photo library");
    HWND photo = wait_for_window(exe, L"设置照片", L"SkinDialog");
    if (!photo || !GetClientRect(photo, &size) || size.right != 604 || size.bottom != 394 || !focus_chat(photo))
        return fail("expected the stock photo library");
    if (clear) {
        if (!activate_control(photo, 60, 91)) return fail("could not choose the default avatar");
    } else {
        if (!activate_control(photo, 514, 205)) return fail("could not open the stock avatar picker");
        struct file_picker picker = {0};
        GetWindowThreadProcessId(photo, &picker.pid);
        for (int i = 0; i < 60 && !picker.window; ++i) {
            EnumWindows(match_profile_picker, (LPARAM)&picker);
            if (!picker.window) Sleep(50);
        }
        HWND open = picker.window ? GetDlgItem(picker.window, IDOK) : NULL;
        DWORD_PTR result = 0;
        if (!open || !SendMessageTimeoutW(picker.edit, WM_SETTEXT, 0, (LPARAM)path, SMTO_ABORTIFHUNG, 1000, &result) ||
            !result || !PostMessageW(picker.window, WM_COMMAND, IDOK, (LPARAM)open)) {
            EnumWindows(describe_window, (LPARAM)exe);
            return fail("could not choose the original fixture avatar");
        }
        for (int i = 0; i < 60 && IsWindowVisible(picker.window); ++i) Sleep(50);
        if (IsWindowVisible(picker.window)) return fail("original avatar picker did not close");
    }
    if (!focus_chat(photo)) return fail("could not return to the stock photo library");
    Sleep(850); // the library grows back after the file picker closes
    if (!activate_control(photo, 514, 275)) return fail("could not save the original photo");
    for (int i = 0; i < 60 && IsWindowVisible(photo); ++i) Sleep(50);
    if (IsWindowVisible(photo)) return fail("original photo library did not close");
    if (!focus_chat(profile) || !activate_control(profile, 229, 124)) return fail("could not save the edited profile");
    Sleep(850);
    if (IsWindowVisible(profile)) activate_control(profile, 309, 27);
    return 0;
}

int wmain(int argc, wchar_t **argv) {
    if (argc < 4 || argc > 5) return fail("invalid reference driver arguments");
    const wchar_t *exe = argv[1], *action = argv[2], *chat_name = argv[3];
    if (!wcscmp(action, L"stock-avatar") && argc == 5 && !wcsncmp(chat_name, L"GoIM QA ", 8))
        return stock_avatar(exe, argv[4]);
    if (!wcscmp(action, L"stock-presence") && argc == 5 && !wcsncmp(chat_name, L"GoIM QA ", 8)) {
        int row;
        if (!wcscmp(argv[4], L"online")) row = 100;
        else if (!wcscmp(argv[4], L"busy")) row = 124;
        else if (!wcscmp(argv[4], L"away")) row = 172;
        else return fail("invalid stock test presence");
        dismiss_guide(exe);
        HWND main = wait_for_window(exe, L"单位名称", L"SkinDialog");
        RECT size;
        if (!main || !GetClientRect(main, &size) || size.right != 304 || size.bottom != 574 ||
            !focus_chat(main) || !activate_control(main, 224, 73))
            return fail("could not open the private stock presence menu");
        POINT point = {230, row};
        if (!ClientToScreen(main, &point)) return fail("could not locate the stock presence item");
        DWORD expected_pid = 0, menu_pid = 0;
        GetWindowThreadProcessId(main, &expected_pid);
        for (int i = 0; i < 20; ++i) {
            HWND menu = WindowFromPoint(point);
            if (menu && menu != main && GetWindowThreadProcessId(menu, &menu_pid) && menu_pid == expected_pid &&
                GetWindowRect(menu, &size) && size.right - size.left >= 140 && size.right - size.left <= 195 &&
                ScreenToClient(menu, &point))
                return activate_control(menu, point.x, point.y) ? 0 : fail("could not select the stock presence item");
            Sleep(50);
        }
        return fail("expected the private stock presence popup at its default position");
    }
    BOOL stock_pair = wcsncmp(action, L"stock-", 6) == 0 &&
                      (!wcscmp(chat_name, L"GOIM-NWT") || !wcscmp(chat_name, L"GOIM-NWT-2"));
    if (stock_pair && !wcscmp(action, L"stock-open-chat") && argc == 4) {
        dismiss_guide(exe);
        HWND main = wait_for_window(exe, L"单位名称", L"SkinDialog");
        RECT r;
        if (!main || !GetClientRect(main, &r) || r.right != 304 || r.bottom != 574)
            return fail("expected the stock fixture contact list");
        if (!activate_control(main, 130, 164) || !enter_fixture_text(main, chat_name))
            return fail("could not find the other original fixture");
        Sleep(500);
        PostMessageW(main, WM_KEYDOWN, VK_DOWN, 1);
        PostMessageW(main, WM_KEYDOWN, VK_RETURN, 1);
        if (!wait_for_window(exe, chat_name, L"SkinDialog")) {
            EnumWindows(describe_window, (LPARAM)exe);
            return fail("the other original's chat did not open");
        }
        return 0;
    }
    if (stock_pair && !wcscmp(action, L"stock-send-folder")) action = L"send-folder";
    if (stock_pair && !wcscmp(action, L"stock-send-file")) action = L"send-file";
    if (stock_pair && !wcscmp(action, L"stock-accept-file")) action = L"accept-file";
    if (stock_pair && !wcscmp(action, L"stock-chat-check")) action = L"chat-check";
    if (stock_pair && !wcscmp(action, L"stock-reply")) action = L"reply";
    BOOL group_reply = !wcscmp(action, L"group-reply"), group_check = !wcscmp(action, L"group-check");
    BOOL group_image = !wcscmp(action, L"group-image"), group_open = !wcscmp(action, L"group-open");
    BOOL group_close = !wcscmp(action, L"group-close");
    BOOL group_exit = !wcscmp(action, L"group-exit"), group_exit_seen = !wcscmp(action, L"group-exit-seen");
    BOOL group_recall = !wcsncmp(action, L"group-recall-", 13);
    if (group_reply || group_check || group_image || group_open || group_close || group_recall || group_exit || group_exit_seen) {
        const wchar_t *prefix = L"GOIM-NWT，GoIM QA";
        const wchar_t *owned = L"GoIM QA 群聊 ";
        BOOL owned_name = FALSE;
        if (!wcsncmp(chat_name, owned, wcslen(owned))) {
            const wchar_t *token = chat_name + wcslen(owned);
            owned_name = wcsspn(token, L"0123456789abcdef") == 16 &&
                         (!token[16] || !wcscmp(token + 16, L" 新名"));
        }
        if (!owned_name && (group_open || group_close || wcsncmp(chat_name, prefix, wcslen(prefix))))
            return fail("invalid sandbox group name");
    } else if (!stock_pair && wcsncmp(chat_name, L"GoIM QA ", 8)) return fail("invalid sandbox chat name");
    if (group_open && argc == 4) {
        struct window_query query = {exe, chat_name, L"SkinDialog", NULL};
        EnumWindows(match_window, (LPARAM)&query);
        if (query.result) return 0;
        HWND main = wait_for_window(exe, L"单位名称", L"SkinDialog");
        RECT r;
        if (!main || !GetClientRect(main, &r) || r.right != 304 || r.bottom != 574)
            return fail("expected the stock fixture contact list");
        if (!activate_control(main, 130, 164) || !enter_fixture_text(main, chat_name))
            return fail("could not search for the GoIM group");
        Sleep(500);
        PostMessageW(main, WM_KEYDOWN, VK_DOWN, 1);
        PostMessageW(main, WM_KEYDOWN, VK_RETURN, 1);
        if (wait_for_window(exe, chat_name, L"SkinDialog")) return 0;
        EnumWindows(describe_window, (LPARAM)exe);
        return fail("the GoIM group's chat did not open");
    }
    HWND chat = wait_for_window(exe, chat_name, L"SkinDialog");
    RECT client = {0};
    if (!chat || !GetClientRect(chat, &client) ||
        ((client.right != 874 || client.bottom != 600) &&
         (client.right != 1034 || client.bottom != 624))) {
        EnumWindows(describe_window, (LPARAM)exe);
        return fail("expected original chat window with the stock 3.4.3055 layout");
    }

    // Locations come from the stock chat skin at 96 dpi with a new profile.
    const int composer_x = 230, composer_y = 535, button_y = 581;
    if (argc == 4 && group_exit) return exit_fixture_group(exe, chat);
    if (argc == 4 && group_exit_seen) {
        if (!focus_chat(chat)) return fail("could not read the private group transcript");
        const int positions[] = {220, 135, 320};
        for (unsigned i = 0; i < _countof(positions); ++i) {
            if (copied_transcript_contains(chat, positions[i], L"退出了该群")) return 0;
        }
        return fail("the original did not report the GoIM member's exit");
    }
    const wchar_t *recall_action = group_recall ? action + 13 : !wcsncmp(action, L"chat-recall-", 12) ? action + 12 : L"";
    if (argc == 5 && (!wcscmp(recall_action, L"stock") || !wcscmp(recall_action, L"check") || !wcscmp(recall_action, L"gone"))) {
        const wchar_t *token = argv[4];
        if (wcslen(token) != 16 || wcsspn(token, L"0123456789abcdef") != 16)
            return fail("invalid recall fixture token");
        wchar_t stock[64], target[64], control[64];
        swprintf(stock, _countof(stock), L"原版待撤回 %ls", token);
        swprintf(target, _countof(target), L"GoIM 待撤回 %ls", token);
        swprintf(control, _countof(control), L"原文保留 %ls", token);
        if (!wcscmp(recall_action, L"stock")) return recall_fixture_message(exe, chat, stock, group_recall);
        if (!focus_chat(chat)) return fail("could not read the private recall transcript");
        BOOL gone = !wcscmp(recall_action, L"gone");
        for (int i = 0; i < 4; ++i) {
            if (copied_transcript_matches(chat, 220, control, gone ? target : NULL) &&
                (gone || copied_transcript_contains(chat, 220, target))) return 0;
        }
        return fail("the original transcript did not match the expected recall state");
    }
    if (group_close && argc == 4) {
        if (!PostMessageW(chat, WM_CLOSE, 0, 0)) return fail("could not close the original group chat");
        for (int i = 0; i < 40; ++i) {
            if (!IsWindowVisible(chat)) return 0;
            Sleep(50);
        }
        return fail("the original group chat did not close");
    }
    if ((group_check || !wcscmp(action, L"chat-check")) && argc == 5) {
        const wchar_t *marker = argv[4];
        const wchar_t *prefix = group_check ? L"群聊" : L"离线补投";
        if (wcslen(marker) >= 100 || !wcsstr(marker, prefix)) return fail("invalid sandbox transcript marker");
        if (!focus_chat(chat)) return fail("could not activate the original transcript");
        const int positions[] = {220, 135, 320};
        for (unsigned i = 0; i < _countof(positions); ++i) {
            if (copied_transcript_contains(chat, positions[i], marker)) return 0;
        }
        return fail("the expected message could not be read from the original transcript");
    }
    if ((!wcscmp(action, L"reply") || !wcscmp(action, L"queue-text") || group_reply) && argc == 5) {
        dismiss_guide(exe);
        Sleep(500);
        if (!wcscmp(action, L"reply") && !focus_chat(chat)) return fail("could not activate the original test conversation");
        if (!activate_control(chat, composer_x, composer_y)) return fail("could not focus original composer");
        Sleep(180);
        if (!enter_fixture_text(chat, argv[4])) return fail("could not enter fixture text");
        return activate_control(chat, 680, button_y) ? 0 : fail("could not send fixture text");
    }
    if (!wcscmp(action, L"send-folder") && argc == 5) {
        if (wcscmp(argv[4], L"C:\\users\\nwtlab\\Desktop\\GoIMQA目录") ||
            GetFileAttributesW(argv[4]) == INVALID_FILE_ATTRIBUTES)
            return fail("the original folder fixture is missing");
        if (!activate_control(chat, 363, 486))
            return fail("could not open original folder picker");
        HWND picker = wait_for_window(exe, L"Browse for Folder", L"#32770");
        HWND tree = NULL;
        if (picker) EnumChildWindows(picker, match_folder_tree, (LPARAM)&tree);
        if (!tree) return fail("expected the Wine folder tree");
        DWORD_PTR root = 0, selected = 0;
        if (!SendMessageTimeoutW(tree, TVM_GETNEXTITEM, TVGN_ROOT, 0, SMTO_ABORTIFHUNG, 1000, &root) || !root)
            return fail("could not read fixture folder tree");
        SendMessageTimeoutW(tree, TVM_EXPAND, TVE_EXPAND, (LPARAM)root, SMTO_ABORTIFHUNG, 1000, &selected);
        SendMessageTimeoutW(tree, TVM_SELECTITEM, TVGN_CARET, (LPARAM)root, SMTO_ABORTIFHUNG, 1000, &selected);
        SendMessageTimeoutW(tree, WM_CHAR, L'G', 1, SMTO_ABORTIFHUNG, 1000, &selected);
        if (!SendMessageTimeoutW(tree, TVM_GETNEXTITEM, TVGN_CARET, 0, SMTO_ABORTIFHUNG, 1000, &selected) ||
            !selected || selected == root)
            return fail("could not select the fixture folder from the tree");
        return PostMessageW(picker, WM_COMMAND, IDOK, (LPARAM)GetDlgItem(picker, IDOK)) ?
            0 : fail("could not confirm the fixture folder");
    }
    if ((!wcscmp(action, L"send-file") || !wcscmp(action, L"send-image") || group_image) && argc == 5) {
        BOOL image = !wcscmp(action, L"send-image") || group_image;
        const wchar_t fixture_dir[] = L"C:\\goim-qa\\";
        DWORD attributes = GetFileAttributesW(argv[4]);
        if (wcsncmp(argv[4], fixture_dir, _countof(fixture_dir) - 1) ||
            wcspbrk(argv[4] + _countof(fixture_dir) - 1, L"/\\:") ||
            attributes == INVALID_FILE_ATTRIBUTES || (attributes & FILE_ATTRIBUTE_DIRECTORY))
            return fail("the original file fixture is missing");
        if (stock_pair && wcscmp(argv[4], L"C:\\goim-qa\\GoIMQA-control.bin") &&
            wcscmp(argv[4], L"C:\\goim-qa\\GoIMQA-boundary.bin"))
            return fail("unexpected stock-pair fixture file");
        if (stock_pair && !focus_chat(chat)) return fail("could not activate the stock sender");
        if (!activate_control(chat, image ? 236 : 329, 486))
            return fail("could not open original file picker");
        HWND picker = wait_for_window(exe, image ? L"选择要发送的图片" : L"选择要发送的文件", L"#32770");
        HWND edit = NULL;
        if (picker) EnumChildWindows(picker, match_filename_edit, (LPARAM)&edit);
        if (!edit) {
            EnumWindows(describe_window, (LPARAM)exe);
            if (picker) PostMessageW(picker, WM_COMMAND, IDCANCEL, 0);
            return fail("expected the stock original file picker");
        }
        SetForegroundWindow(picker);
        DWORD_PTR result = 0;
        HWND open = GetDlgItem(picker, IDOK);
        if (!SendMessageTimeoutW(edit, WM_SETTEXT, 0, (LPARAM)argv[4], SMTO_ABORTIFHUNG, 1000, &result) ||
            !result || !open || !PostMessageW(picker, WM_COMMAND, IDOK, (LPARAM)open))
            return fail("could not submit original file picker");
        if (image) {
            Sleep(500);
            if (!activate_control(chat, 680, button_y)) return fail("could not send fixture image");
        }
        return 0;
    }
    if (!wcscmp(action, L"probe-group") && argc == 4) {
        dismiss_guide(exe);
        HWND main = wait_for_window(exe, L"单位名称", L"SkinDialog");
        RECT bounds;
        if (!main || !GetClientRect(main, &bounds) || bounds.right < 250 || bounds.right > 320 ||
            bounds.bottom < 500 || bounds.bottom > 590) {
            EnumWindows(describe_window, (LPARAM)exe);
            return fail("expected the original main window");
        }
        if (!activate_control(main, 192, bounds.bottom - 28))
            return fail("could not open the original group picker");
        HWND wizard = wait_for_window(exe, L"创建群", L"SkinDialog");
        if (!wizard || !GetClientRect(wizard, &bounds) || bounds.right != 564 || bounds.bottom != 439)
            return fail("expected the original group member picker");
        if (!activate_control(wizard, 105, 368) || !enter_fixture_text(wizard, chat_name))
            return fail("could not search for the named test contact");
        Sleep(700);
        PostMessageW(wizard, WM_KEYDOWN, VK_DOWN, 1);
        PostMessageW(wizard, WM_KEYDOWN, VK_RETURN, 1);
        Sleep(350);
        if (!activate_control(wizard, 420, 405)) return fail("could not submit the test contact");
        Sleep(750);
        EnumWindows(describe_window, (LPARAM)exe);
        if (IsWindowVisible(wizard)) {
            activate_control(wizard, 502, 405);
            return fail("group picker did not accept the selected member");
        }
        return 0;
    }
    if (!wcscmp(action, L"accept-file") && argc == 4) {
        if (stock_pair && !focus_chat(chat)) return fail("could not activate the stock receiver");
        Sleep(350); // the reference animates the transfer pane after an offer
        return activate_control(chat, 766, button_y) ? 0 : fail("could not accept fixture file");
    }
    if (!wcscmp(action, L"cancel-file") && argc == 4) {
        Sleep(600); // the stock transfer pane slides out to the right
        return activate_control(chat, 965, button_y) ? 0 : fail("could not cancel fixture file");
    }
    return fail("unrecognized reference driver action");
}
