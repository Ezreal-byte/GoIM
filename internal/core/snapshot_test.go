package core

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	"goim/internal/domain"
)

func TestCloneSnapshotStatePreservesJSONView(t *testing.T) {
	created := time.Date(2026, time.November, 24, 9, 42, 0, 123, time.FixedZone("test", 90*60))
	for _, test := range []struct {
		name     string
		messages []domain.Message
	}{
		{"nil history", nil},
		{"empty history", []domain.Message{}},
		{"text, voice and remote desktop", []domain.Message{
			{ID: "text", Text: "你好", CreatedAt: created},
			{ID: "voice", Voice: &domain.VoiceRecord{Connected: true, DurationSeconds: 21}},
			{ID: "empty record", Voice: &domain.VoiceRecord{}},
			{ID: "remote desktop", RemoteDesktop: &domain.RemoteDesktopRecord{Connected: true, DurationSeconds: 35}},
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			state := domain.State{
				Messages: test.messages,
				Files:    []domain.File{{ID: "file", ShareURL: "local-only-test-value"}},
				Settings: domain.Settings{
					ColorScheme: "legacy",
					LocalSend:   domain.LocalSendConfig{PIN: "test-only"},
				},
			}
			encoded, err := json.Marshal(state)
			if err != nil {
				t.Fatal(err)
			}
			var legacy domain.State
			if err := json.Unmarshal(encoded, &legacy); err != nil {
				t.Fatal(err)
			}
			want, err := json.Marshal(legacy)
			if err != nil {
				t.Fatal(err)
			}
			snapshot := cloneSnapshotState(state)
			got, err := json.Marshal(snapshot)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, want) || snapshot.Files[0].ShareURL != "" || snapshot.Settings.LocalSend.PIN != "" {
				t.Fatal("snapshot no longer matches the previous JSON view")
			}
		})
	}
}

func TestSnapshotKeepsChatHistoryIndependent(t *testing.T) {
	voice := &domain.VoiceRecord{Connected: true, DurationSeconds: 21}
	remoteDesktop := &domain.RemoteDesktopRecord{Connected: true, DurationSeconds: 35}
	now := time.Now()
	node := &Node{data: persisted{State: domain.State{Messages: []domain.Message{
		{ID: "message", Text: "original", CreatedAt: now, Voice: voice, RemoteDesktop: remoteDesktop},
		{ID: "second", Voice: voice, RemoteDesktop: remoteDesktop},
	}}}}
	first, second := node.Snapshot(), node.Snapshot()
	if first.Messages[0].CreatedAt != now.Round(0) {
		t.Fatal("snapshot did not preserve the message time without the monotonic reading")
	}
	first.Messages[0].Text = "edited"
	first.Messages[0].Voice.DurationSeconds = 1
	first.Messages[0].RemoteDesktop.DurationSeconds = 2
	if second.Messages[0].Text != "original" || second.Messages[0].Voice.DurationSeconds != 21 ||
		first.Messages[1].Voice.DurationSeconds != 21 || voice.DurationSeconds != 21 {
		t.Fatal("editing one snapshot changed another message or the stored state")
	}
	if second.Messages[0].RemoteDesktop.DurationSeconds != 35 ||
		first.Messages[1].RemoteDesktop.DurationSeconds != 35 || remoteDesktop.DurationSeconds != 35 {
		t.Fatal("editing one snapshot changed remote desktop history elsewhere")
	}
	node.data.State.Messages[0].Text = "new state"
	if node.Snapshot().Messages[0].Text != "new state" || second.Messages[0].Text != "original" {
		t.Fatal("a stored message update changed an earlier snapshot or was missed")
	}
}
