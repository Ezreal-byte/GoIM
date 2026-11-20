package desktop

import (
	"fmt"
	"github.com/vivaui/viva/app"
	"goim/internal/domain"
	"testing"
	"time"
)

func BenchmarkHistoryWindowPaint(b *testing.B) {
	s := domain.State{Self: domain.Peer{ID: "self", Name: "我"}, Settings: domain.DefaultSettings(b.TempDir()), Conversations: []domain.Conversation{{ID: "conversation", Kind: "direct", PeerID: "peer"}}, Peers: []domain.Peer{{ID: "peer", Name: "同事"}}}
	for i := 0; i < 1000; i++ {
		s.Messages = append(s.Messages, domain.Message{ID: fmt.Sprint(i), ConversationID: "conversation", SenderID: "peer", Kind: "text", Text: "设计评审方案已更新，请核对交互细节。", CreatedAt: time.Now().Add(-time.Duration(i) * time.Minute)})
	}
	v := newHistoryWindowView(newShellTestBackend(s), "conversation")
	defer v.Close()
	v.search.SetText("设计")
	v.filter()
	driver, err := app.NewDriver(v.Config(), nil)
	if err != nil {
		b.Fatal(err)
	}
	defer driver.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if err := driver.Paint(); err != nil {
			b.Fatal(err)
		}
	}
}
