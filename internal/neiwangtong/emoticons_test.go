package neiwangtong

import (
	"bytes"
	"strings"
	"testing"
)

func TestStockEmoticonsPreserveMixedChatText(t *testing.T) {
	const body = "你好🙂 & <中文> 👍 正常正文"
	packet, err := FormatSendMsg(32, body)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(packet, []byte("&quot;1251&quot;")) || !bytes.Contains(packet, []byte("&quot;1297&quot;")) {
		t.Fatal("known emoji were not encoded as the original's native IDs")
	}
	message, err := ParseDirectMessage(packet)
	if err != nil || message.Text() != body {
		t.Fatalf("mixed native text was changed: %q, %v", message.Text(), err)
	}
	for _, part := range message.Parts {
		if part.Image != nil {
			t.Fatal("a built-in emoticon attempted an arbitrary image download")
		}
	}
}

func TestManyEmoticonsKeepWholeMessageWithinPartLimit(t *testing.T) {
	text := strings.Repeat("hi🙂", 100)
	packet, err := FormatSendMsg(33, text)
	if err != nil {
		t.Fatal(err)
	}
	m, err := ParseDirectMessage(packet)
	if err != nil || m.Text() != text || len(m.Parts) > 64 {
		t.Fatalf("long mixed-emoji text was truncated: %d parts, %v", len(m.Parts), err)
	}
}
