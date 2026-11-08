package desktop

import (
	"errors"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/ui"

	"goim/internal/domain"
)

type testWindowGeometry struct {
	frame ui.Rect
	sizes [][2]int
	err   error
}

func (window *testWindowGeometry) Frame() (ui.Rect, bool) {
	return window.frame, true
}

func (window *testWindowGeometry) SetSize(width, height int) error {
	window.sizes = append(window.sizes, [2]int{width, height})
	if window.err == nil {
		window.frame.Width, window.frame.Height = float32(width), float32(height)
	}
	return window.err
}

// chatSurfaceConfig drives drawMainBody, the splitter hooks and the viewer hooks
// as root wires them into the shell. Panel sections and modal overlays are
// outside these component tests.
func chatSurfaceConfig(shell *UI) app.Config {
	config := shell.Config()
	config.Paint = func(canvas *ui.Canvas, size ui.Size) {
		shell.theme = themeForName(shell.state.Settings.Theme)
		shell.palette = paletteFor(shell.theme)
		bounds := ui.Rect{Width: size.Width, Height: size.Height}
		canvas.Fill(bounds, shell.palette.sidebar)
		shell.focusedID = canvas.Focused()
		title := min(titlebarHeight, bounds.Height)
		body := ui.Rect{
			Y: title, Width: bounds.Width,
			Height: max(bounds.Height-title, 0),
		}
		shell.lastBody = body
		shell.drawTitlebar(canvas, ui.Rect{Width: bounds.Width, Height: title})
		shell.drawMainBody(canvas, body)
		shell.hover.Paint(canvas, shell.theme, body, components.TooltipOptions{Layer: 55}, 0)
		shell.drawOverlays(canvas, bounds)
		shell.drawChatImageViewer(canvas)
	}
	config.OnKey = func(event ui.KeyEvent) ui.EventResult {
		if result, handled := shell.chatOverlayKey(event); handled {
			return result
		}
		return ui.EventResult{}
	}
	config.OnShortcut = func(event ui.KeyEvent) ui.EventResult {
		if result, handled := shell.chatOverlayShortcut(event); handled {
			return result
		}
		return ui.EventResult{}
	}
	config.OnScroll = func(event ui.ScrollEvent) ui.EventResult {
		if result, handled := shell.chatOverlayWheel(event); handled {
			return result
		}
		return ui.EventResult{}
	}
	config.OnMaximize = shell.setWindowMaximized
	return config
}

func newChatSurfaceHarness(t *testing.T, state domain.State, width, height int) *shellTestHarness {
	t.Helper()
	backend := newShellTestBackend(state)
	shell := New(backend)
	config := chatSurfaceConfig(shell)
	config.Width, config.Height = width, height
	driver, err := app.NewDriver(config, nil)
	if err != nil {
		shell.Close()
		t.Fatal(err)
	}
	harness := &shellTestHarness{t: t, ui: shell, backend: backend, driver: driver}
	shell.invalidator.Invalidate()
	t.Cleanup(shell.Close)
	t.Cleanup(driver.Close)
	return harness
}

func (harness *shellTestHarness) dragID(id ui.ID, dx, dy float32) {
	harness.t.Helper()
	hit, ok := harness.findHit(id)
	if !ok {
		harness.t.Fatalf("control %v is not on screen", id)
	}
	x := hit.Bounds.X + hit.Bounds.Width/2
	y := hit.Bounds.Y + hit.Bounds.Height/2
	if err := harness.driver.Press(x, y, ui.PointerButtonPrimary); err != nil {
		harness.t.Fatal(err)
	}
	if err := harness.driver.Move(x+dx, y+dy); err != nil {
		harness.t.Fatal(err)
	}
	if err := harness.driver.Release(x+dx, y+dy, ui.PointerButtonPrimary); err != nil {
		harness.t.Fatal(err)
	}
}

func TestChatSidebarSplitterDragsAndMovesWithKeyboard(t *testing.T) {
	harness := newChatSurfaceHarness(t, shellTestState(t), 1440, 960)
	surface := harness.ui.chatSurface()
	hit, ok := harness.findHit(surface.ids.sidebarSplitter)
	if !ok {
		t.Fatal("sidebar splitter is missing")
	}
	if hit.Bounds.Width < chatSplitterExtent-0.01 {
		t.Fatalf("sidebar splitter is %.1fpx wide, want at least %.0f", hit.Bounds.Width, chatSplitterExtent)
	}

	before := surface.sidebar.Pos
	harness.dragID(surface.ids.sidebarSplitter, 64, 0)
	if surface.sidebar.Pos <= before+40 {
		t.Fatalf("sidebar drag moved %.1fpx, want about 64px", surface.sidebar.Pos-before)
	}
	if surface.sidebar.Pos > chatSidebarMaxWidth {
		t.Fatalf("sidebar width %.1f exceeds its maximum %.0f", surface.sidebar.Pos, chatSidebarMaxWidth)
	}

	boundary := harness.ui.chatSidebarBoundary()
	search, ok := harness.findHit(harness.ui.ids.sidebarSearch)
	if !ok || search.Bounds.X < railWidth || search.Bounds.X+search.Bounds.Width > boundary+0.01 {
		t.Fatalf("sidebar search escaped the resized sidebar: %+v, boundary %.1f", search.Bounds, boundary)
	}
	header, ok := harness.findHit(harness.ui.ids.chatHeaderFile)
	if !ok || header.Bounds.X < boundary {
		t.Fatalf("chat controls did not follow the sidebar: %+v, boundary %.1f", header.Bounds, boundary)
	}

	harness.clickID(surface.ids.sidebarSplitter)
	before = surface.sidebar.Pos
	if err := harness.driver.Key(ui.KeyLeft, 0); err != nil {
		t.Fatal(err)
	}
	if surface.sidebar.Pos != before-16 {
		t.Fatalf("sidebar keyboard step moved %.1fpx, want 16", surface.sidebar.Pos-before)
	}

	harness.dragID(surface.ids.sidebarSplitter, chatSidebarMinWidth-surface.sidebar.Pos-40, 0)
	if surface.sidebar.Pos != chatSidebarMinWidth {
		t.Fatalf("sidebar did not clamp at its minimum: %.1f", surface.sidebar.Pos)
	}
	requireShellLayout(t, harness.snapshot())
	harness.dragID(surface.ids.sidebarSplitter, chatSidebarMaxWidth-surface.sidebar.Pos+40, 0)
	if surface.sidebar.Pos != chatSidebarMaxWidth {
		t.Fatalf("sidebar did not clamp at its maximum: %.1f", surface.sidebar.Pos)
	}
	requireShellLayout(t, harness.snapshot())

	if err := harness.driver.Resize(1000, 700); err != nil {
		t.Fatal(err)
	}
	harness.settle()
	if harness.ui.lastChat.Width < chatWorkspaceMinWidth-0.01 {
		t.Fatalf("chat workspace shrank to %.1f, want at least %.0f",
			harness.ui.lastChat.Width, chatWorkspaceMinWidth)
	}
	if surface.sidebar.Pos > chatSidebarMaxWidth ||
		surface.sidebar.Pos < chatSidebarMinWidth {
		t.Fatalf("sidebar escaped its bounds after resize: %.1f", surface.sidebar.Pos)
	}
	requireShellLayout(t, harness.snapshot())
}

func TestFilesPaneExpandsAndRestoresANormalWindow(t *testing.T) {
	harness := newChatSurfaceHarness(t, shellTestState(t), 1440, 960)
	harness.ui.selected = "conv-group"
	window := &testWindowGeometry{frame: ui.Rect{Width: 1440, Height: 960}}
	harness.ui.attachNativeWindow(window)
	harness.ui.toggleFilesPane()
	if len(window.sizes) != 1 || window.sizes[0] != [2]int{1840, 960} {
		t.Fatalf("open sizes = %v", window.sizes)
	}
	harness.ui.toggleFilesPane()
	if len(window.sizes) != 2 || window.sizes[1] != [2]int{1440, 960} {
		t.Fatalf("close sizes = %v", window.sizes)
	}
}

func TestFilesPaneDoesNotResizeMaximizedOrFullscreenWindow(t *testing.T) {
	harness := newChatSurfaceHarness(t, shellTestState(t), 1440, 960)
	harness.ui.selected = "conv-group"
	window := &testWindowGeometry{frame: ui.Rect{Width: 1920, Height: 1040}}
	harness.ui.attachNativeWindow(window)
	harness.ui.setWindowMaximized(true)
	harness.ui.toggleFilesPane()
	if len(window.sizes) != 0 {
		t.Fatalf("maximized window sizes = %v", window.sizes)
	}
	harness.ui.closeDrawer()
	harness.ui.setWindowMaximized(false)
	harness.ui.setWindowFullscreen(true)
	harness.ui.toggleFilesPane()
	if len(window.sizes) != 0 {
		t.Fatalf("fullscreen window sizes = %v", window.sizes)
	}
}

func TestFilesPaneSizeFailureLeavesTheWindowAlone(t *testing.T) {
	harness := newChatSurfaceHarness(t, shellTestState(t), 1440, 960)
	harness.ui.selected = "conv-group"
	window := &testWindowGeometry{frame: ui.Rect{Width: 1440, Height: 960}, err: errors.New("resize failed")}
	harness.ui.attachNativeWindow(window)
	harness.ui.toggleFilesPane()
	surface := harness.ui.chatSurface()
	if len(window.sizes) != 1 || surface.filesWindowRestoreValid || surface.filesWindowExpanded {
		t.Fatalf("failed resize left state: sizes=%v valid=%t expanded=%t",
			window.sizes, surface.filesWindowRestoreValid, surface.filesWindowExpanded)
	}
}

func TestChatHistorySplitterKeepsBothPanesUsable(t *testing.T) {
	harness := newChatSurfaceHarness(t, shellTestState(t), 1440, 960)
	surface := harness.ui.chatSurface()
	if _, ok := harness.findHit(surface.ids.historySplitter); !ok {
		t.Fatal("history/composer splitter is missing")
	}
	initial := surface.history.Pos
	if initial <= chatHistoryMinHeight {
		t.Fatalf("initial history pane is %.1fpx, want above the minimum", initial)
	}

	harness.dragID(surface.ids.historySplitter, 0, -120)
	if surface.history.Pos > initial-100 {
		t.Fatalf("history pane moved %.1fpx, want about -120px", surface.history.Pos-initial)
	}

	harness.dragID(surface.ids.historySplitter, 0, -10000)
	if surface.history.Pos != chatHistoryMinHeight {
		t.Fatalf("history pane minimum is %.1f, want %.0f", surface.history.Pos, chatHistoryMinHeight)
	}

	harness.dragID(surface.ids.historySplitter, 0, 10000)
	maxHistory := float32(960-titlebarHeight) - (chatHeaderExtent + chatDividerExtent) -
		chatSplitterExtent - chatComposerMinHeight
	if surface.history.Pos != maxHistory {
		t.Fatalf("history pane maximum is %.1f, want %.0f", surface.history.Pos, maxHistory)
	}

	harness.clickID(surface.ids.historySplitter)
	if err := harness.driver.Key(ui.KeyHome, 0); err != nil {
		t.Fatal(err)
	}
	if surface.history.Pos != chatHistoryMinHeight {
		t.Fatalf("Home did not reach the history minimum: %.1f", surface.history.Pos)
	}
	if err := harness.driver.Key(ui.KeyEnd, 0); err != nil {
		t.Fatal(err)
	}
	if surface.history.Pos != maxHistory {
		t.Fatalf("End did not reach the history maximum: %.1f", surface.history.Pos)
	}

	harness.clickID(harness.ui.ids.chatHeaderSearch)
	harness.settle()
	searchMax := maxHistory - chatSearchExtent
	if surface.history.Pos != searchMax {
		t.Fatalf("search did not make room in the history pane: %.1f, want %.1f",
			surface.history.Pos, searchMax)
	}
	requireShellLayout(t, harness.snapshot())
	harness.clickID(harness.ui.ids.chatSearchClose)
	harness.settle()
	if surface.history.Pos != maxHistory {
		t.Fatalf("closing search did not restore the history pane: %.1f, want %.1f (open %v, clamp %.1f, before %.1f, searchOpen %v)",
			surface.history.Pos, maxHistory, harness.ui.chatFind.open,
			surface.historyClamp, surface.historyBefore, surface.searchOpen)
	}

	editor, ok := harness.findHit(harness.ui.ids.chatEditor)
	if !ok || editor.Bounds.Height < 40 {
		t.Fatalf("composer editor is not usable at the splitter limit: %+v", editor.Bounds)
	}
	requireShellLayout(t, harness.snapshot())
}

func TestChatFilesPaneDocksClosesAndCompressesChat(t *testing.T) {
	harness := newChatSurfaceHarness(t, shellTestState(t), 1440, 960)
	harness.clickID(harness.ui.ids.chatHeaderFile)
	harness.settle()
	if harness.ui.drawer != "files" {
		t.Fatalf("files drawer = %q, want docked files pane", harness.ui.drawer)
	}
	surface := harness.ui.chatSurface()
	pane := surface.filesPane
	if pane.Width < chatFilesMinWidth || pane.Width > chatFilesMaxWidth {
		t.Fatalf("docked pane width %.1f is outside %.0f..%.0f", pane.Width, chatFilesMinWidth, chatFilesMaxWidth)
	}
	if pane.X+pane.Width > 1440.01 || pane.Y < titlebarHeight {
		t.Fatalf("docked pane is outside the content area: %+v", pane)
	}

	header, ok := harness.findHit(harness.ui.ids.chatHeaderFile)
	if !ok || header.Bounds.X+header.Bounds.Width > pane.X {
		t.Fatalf("chat header did not compress beside the pane: %+v vs %+v", header.Bounds, pane)
	}
	editor, ok := harness.findHit(harness.ui.ids.chatEditor)
	if !ok || editor.Bounds.X+editor.Bounds.Width > pane.X+0.01 {
		t.Fatalf("chat composer did not compress beside the pane: %+v vs %+v", editor.Bounds, pane)
	}
	search, ok := harness.findHit(harness.ui.ids.fileSearch)
	if !ok || search.Bounds.X < pane.X || search.Bounds.X+search.Bounds.Width > pane.X+pane.Width+0.01 {
		t.Fatalf("file search is outside the docked pane: %+v vs %+v", search.Bounds, pane)
	}

	harness.clickID(harness.ui.fileRowIDs["file-1"])
	harness.settle()
	if harness.ui.activeFileID != "file-1" || !harness.hasText("文件详情") {
		t.Fatal("docked pane did not show the selected file detail")
	}
	if err := harness.driver.Key(ui.KeyEscape, 0); err != nil {
		t.Fatal(err)
	}
	harness.settle()
	if harness.ui.activeFileID != "" || !harness.hasText("聊天文件") {
		t.Fatal("Escape did not return file detail to the docked list")
	}
	harness.clickID(harness.ui.fileRowIDs["file-1"])
	harness.settle()
	harness.clickID(harness.ui.ids.fileBack)
	harness.settle()
	if harness.ui.activeFileID != "" {
		t.Fatal("the file detail back button did not return to the list")
	}

	if err := harness.driver.Key(ui.KeyEscape, 0); err != nil {
		t.Fatal(err)
	}
	harness.settle()
	if harness.ui.drawer != "" || harness.ui.chatSurface().filesPane.Width != 0 {
		t.Fatal("Escape did not close the docked pane and restore the chat column")
	}
	harness.clickID(harness.ui.ids.chatHeaderFile)
	harness.settle()
	harness.clickID(harness.ui.ids.fileClose)
	harness.settle()
	if harness.ui.drawer != "" || harness.ui.chatSurface().filesPane.Width != 0 {
		t.Fatal("the files pane close button did not restore the chat column")
	}
	requireShellLayout(t, harness.snapshot())
}

func TestChatFilesPaneCompressesWhenMaximizedAtMinimumSize(t *testing.T) {
	harness := newChatSurfaceHarness(t, shellTestState(t), 1000, 700)
	if err := harness.driver.SetMaximized(true); err != nil {
		t.Fatal(err)
	}
	if !harness.ui.chatSurface().maximized {
		t.Fatal("maximize state was not reported to the chat surface")
	}
	harness.clickID(harness.ui.ids.chatHeaderFile)
	harness.settle()
	pane := harness.ui.chatSurface().filesPane
	if pane.Width < chatFilesMinWidth || pane.Width > chatFilesMaxWidth {
		t.Fatalf("maximized pane width %.1f is outside %.0f..%.0f", pane.Width, chatFilesMinWidth, chatFilesMaxWidth)
	}
	header, ok := harness.findHit(harness.ui.ids.chatHeaderFile)
	if !ok || header.Bounds.X+header.Bounds.Width > pane.X {
		t.Fatalf("maximized chat did not compress beside the pane: %+v vs %+v", header.Bounds, pane)
	}
	if pane.X+pane.Width > 1000.01 || pane.Y+pane.Height > 700.01 {
		t.Fatalf("maximized pane is outside the window: %+v", pane)
	}
	requireShellLayout(t, harness.snapshot())
}

func TestChatLayoutDarkThemeKeepsPaneAndSplitterHits(t *testing.T) {
	state := shellTestState(t)
	state.Settings.Theme = "dark"
	harness := newChatSurfaceHarness(t, state, 1440, 960)
	harness.clickID(harness.ui.ids.chatHeaderFile)
	harness.settle()
	surface := harness.ui.chatSurface()
	if surface.filesPane.Width <= 0 {
		t.Fatal("dark theme did not dock the files pane")
	}
	if _, ok := harness.findHit(surface.ids.sidebarSplitter); !ok {
		t.Fatal("dark theme lost the sidebar splitter")
	}
	if _, ok := harness.findHit(surface.ids.historySplitter); !ok {
		t.Fatal("dark theme lost the history splitter")
	}
	if !harness.ui.isDark() {
		t.Fatal("dark test state painted with the light palette")
	}
	requireShellLayout(t, harness.snapshot())
}

func BenchmarkChatMainBodyPaint(b *testing.B) {
	now := time.Now()
	state := domain.State{
		Self: domain.Peer{ID: "self", Name: "我", Online: true},
		Peers: []domain.Peer{
			{ID: "peer-a", Name: "Alice", Online: true},
			{ID: "peer-b", Name: "Bob", Online: true},
		},
		Conversations: []domain.Conversation{
			{ID: "conv-a", Kind: "direct", PeerID: "peer-a", Title: "Alice", UpdatedAt: now, Preview: "hello"},
		},
		Messages: []domain.Message{
			{ID: "message-a", ConversationID: "conv-a", SenderID: "peer-a", Kind: "text", Text: "hello", CreatedAt: now, Status: "delivered"},
			{ID: "message-b", ConversationID: "conv-a", SenderID: "self", Kind: "text", Text: "ready", CreatedAt: now.Add(time.Second), Status: "sent"},
		},
		Settings: domain.DefaultSettings(b.TempDir()),
		Revision: 1,
	}
	shell := New(newShellTestBackend(state))
	config := chatSurfaceConfig(shell)
	config.Width, config.Height = 1440, 960
	driver, err := app.NewDriver(config, nil)
	if err != nil {
		shell.Close()
		b.Fatal(err)
	}
	defer driver.Close()
	defer shell.Close()

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		shell.invalidator.Invalidate()
		if err := driver.Pump(); err != nil {
			b.Fatal(err)
		}
	}
}
