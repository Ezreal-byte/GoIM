// Test fixture for the original 3.4.3055 client in a private Wine display.
// Its skin paints custom widgets. Use the stock layout in a fresh profile
// and require the expected process, window title and dimensions.
#include <windows.h>
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

static void dismiss_guide(const wchar_t *exe) {
    struct window_query guide = {exe, L"新手引导", L"SkinDialog", NULL};
    EnumWindows(match_window, (LPARAM)&guide);
    if (guide.result) {
        PostMessageW(guide.result, WM_CLOSE, 0, 0);
        Sleep(350);
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

static BOOL CALLBACK match_filename_edit(HWND window, LPARAM arg) {
    wchar_t class_name[32];
    if (GetDlgCtrlID(window) != 1148 || !IsWindowVisible(window) ||
        !GetClassNameW(window, class_name, _countof(class_name)) || wcscmp(class_name, L"Edit"))
        return TRUE;
    *(HWND *)arg = window;
    return FALSE;
}

static int fail(const char *message) {
    fputs(message, stderr);
    fputc('\n', stderr);
    return 1;
}

int wmain(int argc, wchar_t **argv) {
    if (argc < 4 || argc > 5) return fail("invalid reference driver arguments");
    const wchar_t *exe = argv[1], *action = argv[2], *chat_name = argv[3];
    if (wcsncmp(chat_name, L"GoIM QA ", 8)) return fail("invalid sandbox chat name");
    HWND chat = wait_for_window(exe, chat_name, L"SkinDialog");
    RECT client;
    if (!chat || !GetClientRect(chat, &client) || client.right != 874 || client.bottom != 600)
        return fail("expected original chat window with the stock 3.4.3055 layout");

    // Locations come from the stock chat skin at 96 dpi with a new profile.
    const int composer_x = 230, composer_y = 535, button_y = 581;
    if (!wcscmp(action, L"reply") && argc == 5) {
        dismiss_guide(exe);
        Sleep(500);
        if (!focus_chat(chat)) return fail("could not activate the original test conversation");
        if (!activate_control(chat, composer_x, composer_y)) return fail("could not focus original composer");
        Sleep(180);
        for (const wchar_t *p = argv[4]; *p; ++p) {
            DWORD_PTR result;
            if (!SendMessageTimeoutW(chat, WM_CHAR, *p, 1, SMTO_ABORTIFHUNG, 1000, &result))
                return fail("could not enter fixture text");
        }
        return activate_control(chat, 680, button_y) ? 0 : fail("could not send fixture text");
    }
    if (!wcscmp(action, L"send-file") && argc == 5) {
        const wchar_t fixture_dir[] = L"C:\\goim-qa\\";
        if (wcsncmp(argv[4], fixture_dir, _countof(fixture_dir) - 1) ||
            wcspbrk(argv[4] + _countof(fixture_dir) - 1, L"/\\:") ||
            GetFileAttributesW(argv[4]) == INVALID_FILE_ATTRIBUTES)
            return fail("the original file fixture is missing");
        if (!activate_control(chat, 329, 486)) return fail("could not open original file picker");
        HWND picker = wait_for_window(exe, L"选择要发送的文件", L"#32770");
        HWND edit = NULL;
        if (picker) EnumChildWindows(picker, match_filename_edit, (LPARAM)&edit);
        if (!edit) return fail("expected the stock original file picker");
        SetForegroundWindow(picker);
        DWORD_PTR result = 0;
        HWND open = GetDlgItem(picker, IDOK);
        if (!SendMessageTimeoutW(edit, WM_SETTEXT, 0, (LPARAM)argv[4], SMTO_ABORTIFHUNG, 1000, &result) ||
            !result || !open || !PostMessageW(picker, WM_COMMAND, IDOK, (LPARAM)open))
            return fail("could not submit original file picker");
        return 0;
    }
    if (!wcscmp(action, L"accept-file") && argc == 4) {
        Sleep(350); // the reference animates the transfer pane after an offer
        return activate_control(chat, 766, button_y) ? 0 : fail("could not accept fixture file");
    }
    return fail("unrecognized reference driver action");
}
