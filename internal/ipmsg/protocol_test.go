package ipmsg

import (
	"fmt"
	"strings"
	"testing"

	"golang.org/x/text/encoding/simplifiedchinese"
)

func TestFeiQExtendedHeaderUsesFullPacketNumber(t *testing.T) {
	p, err := parsePacket([]byte("1_lbt6_8#998#FeiX#0#0#0#4001#9:1523368691537:def:TINCHER:288:\xba\xc3\x00"))
	if err != nil {
		t.Fatal(err)
	}
	message, _ := decodeMessage(p.Extra, p.has(optionUTF8))
	if p.Number != 1523368691537 || p.baseCommand() != commandSend || message != "好" {
		t.Fatalf("decoded packet = %+v, message %q", p, message)
	}
}

func TestParsePacketUsesProtocolDecimalCommand(t *testing.T) {
	packet, err := parsePacket([]byte("1:100:shirouzu:jupiter:32:Hello"))
	if err != nil {
		t.Fatal(err)
	}
	if packet.Number != 100 || packet.Sender != "shirouzu" || packet.Host != "jupiter" {
		t.Fatalf("unexpected packet: %+v", packet)
	}
	if packet.baseCommand() != commandSend || packet.Extra != "Hello" {
		t.Fatalf("SENDMSG was not parsed from decimal command: %+v", packet)
	}
}

func TestPacketStringRoundTripsOptions(t *testing.T) {
	command := uint32(commandSend) | optionUTF8 | optionFile | optionSendCheck
	packet, err := parsePacket(packetString(42, "sender", "host", command, "hello"))
	if err != nil {
		t.Fatal(err)
	}
	if packet.Command != command || packet.baseCommand() != commandSend {
		t.Fatalf("round-trip command = %d, want %d", packet.Command, command)
	}
	if !packet.has(optionUTF8) || !packet.has(optionFile) || !packet.has(optionSendCheck) {
		t.Fatalf("round-trip lost options: %x", packet.Command)
	}
	if packet.Number != 42 || packet.Extra != "hello" {
		t.Fatalf("round-trip packet changed: %+v", packet)
	}
}

func TestParseCommandAcceptsExplicitHexForCompatibility(t *testing.T) {
	value, err := parseCommand("0x00000020")
	if err != nil || value != commandSend {
		t.Fatalf("parseCommand = %x, %v", value, err)
	}
}

func TestGB18030SenderAndMessageDecode(t *testing.T) {
	sender, err := simplifiedchinese.GB18030.NewEncoder().Bytes([]byte("飞秋用户"))
	if err != nil {
		t.Fatal(err)
	}
	message, err := simplifiedchinese.GB18030.NewEncoder().Bytes([]byte("你好，局域网"))
	if err != nil {
		t.Fatal(err)
	}
	payload := append([]byte("1:9:"), sender...)
	payload = append(payload, []byte(":legacy-host:")...)
	payload = append(payload, []byte(fmt.Sprint(commandSend))...)
	payload = append(payload, ':')
	payload = append(payload, message...)
	packet, err := parsePacket(payload)
	if err != nil {
		t.Fatal(err)
	}
	messageText, _ := decodeMessage(packet.Extra, packet.has(optionUTF8))
	if packet.Sender != "飞秋用户" || messageText != "你好，局域网" {
		t.Fatalf("legacy text decode = %q / %q", packet.Sender, messageText)
	}
}

func TestAttachmentsSupportColonAndControlSafeName(t *testing.T) {
	files := []Attachment{{
		ID: 7, Name: `报告:2026\q3?.pdf`, Size: 123, ModTime: 456, Kind: fileRegular,
	}}
	decoded := decodeAttachments(encodeAttachments(files))
	if len(decoded) != 1 {
		t.Fatalf("decoded %d attachments", len(decoded))
	}
	if decoded[0].ID != 7 || decoded[0].Size != 123 || decoded[0].Kind != fileRegular {
		t.Fatalf("attachment fields changed: %+v", decoded[0])
	}
	if strings.ContainsAny(decoded[0].Name, `:\/?`) || decoded[0].Name != "q3_.pdf" {
		t.Fatalf("unsafe or incorrectly escaped filename: %q", decoded[0].Name)
	}
}

func TestParseFileRequestUsesHexFields(t *testing.T) {
	packet, file, offset, err := parseFileRequest("a:b:10")
	if err != nil {
		t.Fatal(err)
	}
	if packet != 10 || file != 11 || offset != 16 {
		t.Fatalf("request = %d/%d/%d", packet, file, offset)
	}
}
