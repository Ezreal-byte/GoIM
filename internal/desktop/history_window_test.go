package desktop

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
	"goim/internal/domain"
)

func TestHistoryWindowUsesLocalRemarkAndKeepsUsername(t *testing.T) {
	state := shellTestState(t)
	state.Peers[0].Remark = "设计同事"
	v := newHistoryWindowView(newShellTestBackend(state), "conv-alice")
	defer v.Close()
	if !strings.Contains(v.title, "设计同事") || v.senderLabel("peer-a") != "设计同事" {
		t.Fatal("history title or sender filter ignored the local remark")
	}
	if len(v.rows) != 1 || v.rows[0].name != "设计同事" || v.rows[0].username != "Alice" {
		t.Fatalf("history row lost the remark or username: %+v", v.rows)
	}
	driver, err := app.NewDriver(v.Config(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer driver.Close()
	var remark, username gfx.Text
	for _, label := range driver.Snapshot().Texts {
		if label.Value == "设计同事" {
			remark = label
		} else if label.Value == "Alice" {
			username = label
		}
	}
	if remark.Value == "" || username.Value == "" || remark.Style.Size <= username.Style.Size || remark.Bounds.X >= username.Bounds.X {
		t.Fatal("history row did not show the username secondarily after the remark")
	}
}

func TestHistoryWindowRequestPreservesConversationAndComposer(t *testing.T) {
	h := newShellTestHarnessAtSize(t, shellTestState(t), 1440, 960)
	h.typeIntoLabel("消息输入框", "未发送的草稿")
	composer, _ := h.chatEditorHit()
	timeline := h.ui.timeline
	requested := ""
	h.ui.presentHistory = func(id string) bool { requested = id; return true }
	h.clickID(h.ui.ids.chatHeaderSearch)
	if requested != h.ui.selected || h.ui.chatFind.open {
		t.Fatal("search did not request an independent history window")
	}
	if h.ui.chatEditor.String() != "未发送的草稿" || h.ui.timeline != timeline {
		t.Fatal("history search changed the draft or timeline")
	}
	after, _ := h.chatEditorHit()
	if composer.Bounds != after.Bounds {
		t.Fatal("opening history changed the composer geometry")
	}
	requested = ""
	if handled, err := h.driver.ShortcutChord(gfx.ModifierControl, 'f'); err != nil || !handled || requested != h.ui.selected {
		t.Fatalf("Ctrl+F = %v, %v, %q", handled, err, requested)
	}
}

func historyTestState(t *testing.T) domain.State {
	s := shellTestState(t)
	now := time.Now()
	s.Messages = append(s.Messages,
		domain.Message{ID: "history-link", ConversationID: "conv-group", SenderID: "peer-a", Kind: "text", Text: "设计资料 https://example.com/design", CreatedAt: now},
		domain.Message{ID: "voice:history", ConversationID: "conv-group", SenderID: "self", Kind: "system", Text: "语音通话 · 通话时长 00:06", CreatedAt: now.Add(-time.Minute), Voice: &domain.VoiceRecord{Direction: domain.VoiceCallOutgoing, Connected: true, DurationSeconds: 6}},
		domain.Message{ID: "history-hidden", ConversationID: "conv-group", SenderID: "self", Kind: "text", Text: "已删除的内容", CreatedAt: now, Deleted: true},
		domain.Message{ID: "history-old", ConversationID: "conv-group", SenderID: "peer-a", Kind: "text", Text: "上个月的设计资料", CreatedAt: now.AddDate(0, -2, 0)},
	)
	return s
}

func TestHistoryFiltersAndNativeTextInput(t *testing.T) {
	backend := newShellTestBackend(historyTestState(t))
	v := newHistoryWindowView(backend, "conv-group")
	defer v.Close()
	driver, err := app.NewDriver(v.Config(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer driver.Close()
	if err = driver.Type("设计资料"); err != nil {
		t.Fatal(err)
	}
	if len(v.rows) != 2 || v.search.Text() != "设计资料" {
		t.Fatalf("text search = %q / %d", v.search.Text(), len(v.rows))
	}
	if err = driver.Key(gfx.KeyEnter, 0); err != nil || v.search.Text() != "设计资料" {
		t.Fatalf("Enter inserted a line break into search: %q / %v", v.search.Text(), err)
	}
	v.date = time.Now().AddDate(0, -2, 0).Format("2006-01-02")
	v.filter()
	if len(v.rows) != 1 || v.rows[0].message.ID != "history-old" {
		t.Fatal("specific date did not find the old record")
	}
	v.date = "today"
	v.filter()
	if len(v.rows) != 1 {
		t.Fatalf("date filter = %d", len(v.rows))
	}
	v.sender = "self"
	v.filter()
	if len(v.rows) != 0 {
		t.Fatal("sender filter did not intersect the query")
	}
	v.sender = ""
	v.date = "all"
	v.search.SetText("")
	for _, tc := range []struct{ tab, id string }{{"files", "message-file"}, {"links", "history-link"}, {"calls", "voice:history"}} {
		v.tab = tc.tab
		v.filter()
		if len(v.rows) != 1 {
			t.Fatalf("tab %s has %d rows", tc.tab, len(v.rows))
		}
		if tc.tab != "files" && v.rows[0].message.ID != tc.id {
			t.Fatalf("tab %s shows wrong record", tc.tab)
		}
	}
	v.tab = "all"
	v.filter()
	for _, r := range v.rows {
		if r.message.Deleted {
			t.Fatal("deleted message appeared in results")
		}
	}
	closed := false
	v.close = func() { closed = true }
	if err = driver.Key(gfx.KeyEscape, 0); err != nil || !closed {
		t.Fatalf("Escape failed: %v", err)
	}
	v.state.Locked = true
	v.wake.Invalidate()
	// Render the locked state directly, as the runtime also hides the OS window.
	var canvas gfx.Canvas
	v.paint(&canvas, gfx.Size{Width: 820, Height: 880})
	if len(canvas.Texts()) != 0 {
		t.Fatal("locked history leaked content")
	}
}

func TestHistoryLocateRevealsEarlierMessage(t *testing.T) {
	s := shellTestState(t)
	for i := 0; i < 60; i++ {
		s.Messages = append(s.Messages, domain.Message{ID: fmt.Sprintf("history-%d", i), ConversationID: "conv-group", SenderID: "peer-a", Kind: "text", Text: fmt.Sprintf("定位测试记录 %d", i), CreatedAt: time.Now().Add(time.Duration(i) * time.Minute)})
	}
	h := newShellTestHarnessAtSize(t, s, 1440, 960)
	h.ui.locateChatMessage("conv-group", "history-5")
	h.settle()
	hit, ok := h.findHit(h.ui.messageIDs["history-5"])
	if !ok || hit.Bounds.Y < h.ui.lastChat.Y || hit.Bounds.Bottom() > h.ui.composerBox.Rect().Y {
		t.Fatalf("located message is not visible: %+v", hit)
	}
	if h.ui.locateMessageID != "" || h.ui.highlightMessageID != "history-5" {
		t.Fatal("locate did not settle and highlight its target")
	}
}

func TestHistoryWindowScreenshotQA(t *testing.T) {
	directory := os.Getenv("GOIM_DESIGN_AUDIT")
	for _, theme := range []string{"light", "dark"} {
		for _, font := range []int{14, 20, 28} {
			t.Run(fmt.Sprintf("%s-%d", theme, font), func(t *testing.T) {
				s := historyTestState(t)
				s.Settings.Theme = theme
				s.Settings.InterfaceFontSize = font
				source := New(newShellTestBackend(s))
				defer source.Close()
				v := newHistoryWindowView(source.backend, "conv-group")
				defer v.Close()
				v.avatar = source.paintPrintAvatar
				cfg := v.Config()
				cfg.Width = 820
				cfg.Height = 880
				session, err := headless.Open(cfg, nil, headless.Options{Scale: 1.5})
				if err != nil {
					t.Fatal(err)
				}
				defer session.Close()
				shot := func(suffix string) {
					t.Helper()
					if err := session.Expect("layout", "clean"); err != nil {
						t.Fatal(err)
					}
					if directory != "" {
						if err := os.MkdirAll(directory, 0755); err != nil {
							t.Fatal(err)
						}
						if err := session.Screenshot(filepath.Join(directory, fmt.Sprintf("history-%s-%d%s.png", theme, font, suffix))); err != nil {
							t.Fatal(err)
						}
					}
				}
				shot("-all")
				if err := session.Type("设计"); err != nil {
					t.Fatal(err)
				}
				shot("-search")
				selected := ""
				v.locate = func(c, m string) { selected = m }
				if len(v.rows) == 0 {
					t.Fatal("search produced no rows")
				}
				v.list.Select(0)
				v.wake.Invalidate()
				if err := session.Wait(10 * time.Millisecond); err != nil {
					t.Fatal(err)
				}
				if err := session.ClickLabel("定位到聊天"); err != nil {
					t.Fatal(err)
				}
				if selected == "" {
					t.Fatal("locate action did not run")
				}
				if err := session.Resize(640, 560); err != nil {
					t.Fatal(err)
				}
				shot("-compact")
			})
		}
	}
}
