package neiwangtong

import (
	"bytes"
	"math/bits"
	"testing"
	"time"
)

func TestStockProfileChangeCarries64BitSystemFace(t *testing.T) {
	// The original uses a 64-bit identifier even when a separate custom
	// avatar is present. Rejecting that identifier lost the whole update.
	raw := []byte(`<X_CHANGE_INFO docver="1"><NAME>Test contact</NAME><SYS_FACE>36028797018963968</SYS_FACE><USER_FACE id="4020">00000000000000000000000000000000</USER_FACE><STATUS>0</STATUS></X_CHANGE_INFO>`)
	var info profileChange
	if err := decodeNativeXML(raw, "X_CHANGE_INFO", &info); err != nil || !validWireProfile(info.wireProfile) ||
		info.SysFace != 1<<55 || info.UserFace.ID != 4020 {
		t.Fatalf("could not read the original's custom avatar update: %+v, %v", info.wireProfile, err)
	}
}

func TestSendMsgFormatAndParse(t *testing.T) {
	formatted, err := FormatSendMsg(42, "你好，内网通测试")
	if err != nil {
		t.Fatalf("Format error: %v", err)
	}
	msgID, text, id, err := ParseSendMsg(formatted)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if msgID != 42 {
		t.Errorf("Expected msgID 42, got %d", msgID)
	}
	if text != "你好，内网通测试" {
		t.Errorf("Expected text '你好，内网通测试', got %s", text)
	}
	if id == "" {
		t.Error("Expected non-empty uuid")
	}
	for _, element := range []string{
		"<MSG_TIME>0</MSG_TIME>",
		"{\n&nbsp;&nbsp;&nbsp;&quot;app&quot;&nbsp;:&nbsp;&quot;shiyeline&quot;,",
		"}\n</MSG>",
	} {
		if !bytes.Contains(formatted, []byte(element)) {
			t.Errorf("native message is missing exact 3.4.3055 framing %q:\n%s", element, formatted)
		}
	}
}

func TestSendMsgAckFormatAndParse(t *testing.T) {
	const msgID uint64 = 18446744073709551615
	formatted := FormatSendMsgAck(msgID)
	if string(formatted) != `<X_SEND_MSG_ACK docver="1"><MSG_ID>18446744073709551615</MSG_ID></X_SEND_MSG_ACK>` {
		t.Fatalf("unexpected native acknowledgement: %s", formatted)
	}
	parsed, err := ParseSendMsgAck(formatted)
	if err != nil {
		t.Fatal(err)
	}
	if parsed != msgID {
		t.Fatalf("parsed message ID %d, want %d", parsed, msgID)
	}
}

func TestStockTextRecallDecodesAsControl(t *testing.T) {
	const id = "0123456789abcdef0123456789abcdef"
	const target = "fedcba9876543210fedcba9876543210"
	body := `<X_SEND_MSG docver="1"><MSG_ID>2</MSG_ID><MSG>{&quot;app&quot;:&quot;shiyeline&quot;,&quot;dt&quot;:[{&quot;txt&quot;:{&quot;t&quot;:&quot;compatible&quot;,&quot;v&quot;:&quot;请升级&quot;}},{&quot;retrieve&quot;:{&quot;m&quot;:&quot;{}&quot;,&quot;v&quot;:&quot;` + target + `&quot;}}],&quot;id&quot;:&quot;` + id + `&quot;,&quot;type&quot;:&quot;4&quot;}</MSG></X_SEND_MSG>`
	decoded, err := ParseDirectMessage([]byte(body))
	if err != nil || decoded.ID != id || decoded.RecallID != target || decoded.PacketID != 2 || len(decoded.Parts) != 0 {
		t.Fatalf("native recall was not a separate control: %+v, err=%v", decoded, err)
	}
	bad := bytes.ReplaceAll([]byte(body), []byte(target), []byte("unrelated"))
	if _, err := ParseDirectMessage(bad); err == nil {
		t.Fatal("malformed recall was accepted as chat content")
	}
}

func TestSendFileFormatAndParse(t *testing.T) {
	formatted := FormatSendFile(101, 202, "test.txt", 12345, 1700000000)
	sf, err := ParseSendFile(formatted)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if sf.TaskID != 101 || sf.FileID != 202 || sf.Name != "test.txt" || sf.Size != 12345 {
		t.Errorf("Unexpected SendFile parse: %+v", sf)
	}
}

func TestSendFileUsesCurrentTimestampWhenMissing(t *testing.T) {
	before := time.Now().Unix()
	sf, err := ParseSendFile(FormatSendFile(101, 202, "test.txt", 12345, 0))
	if err != nil {
		t.Fatal(err)
	}
	after := time.Now().Unix()
	decoded := sf.LastModify/10_000_000 - 11_644_473_600
	if decoded < before || decoded > after {
		t.Fatalf("last modify FILETIME %d decodes to %d, want Unix time in [%d,%d]", sf.LastModify, decoded, before, after)
	}
}

func TestOperateRecvFileFormatAndParse(t *testing.T) {
	formatted := FormatOperateRecvFile(555, 2)
	taskID, op, err := ParseOperateRecvFile(formatted)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if taskID != 555 || op != 2 {
		t.Errorf("Unexpected: taskID=%d, op=%d", taskID, op)
	}
}

func TestOperateSendFileParse(t *testing.T) {
	taskID, op, err := ParseOperateSendFile([]byte(`<X_OPERATE_SEND_FILE docver="1"><TASK_ID>18446744073709551615</TASK_ID><OP>2</OP></X_OPERATE_SEND_FILE>`))
	if err != nil {
		t.Fatal(err)
	}
	if taskID != ^uint64(0) || op != 2 {
		t.Fatalf("unexpected send-file operation: task=%d op=%d", taskID, op)
	}
}

func TestHandsharkFormatAndParse(t *testing.T) {
	formatted := FormatHandshark("测试用户", "技术部", "在线中", 0, 9013, 9012, 123456, "abcdef123456")
	hs, err := ParseHandshark(formatted)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if hs.Info.Name != "测试用户" || hs.Info.Group != "技术部" || hs.Info.Sign != "在线中" {
		t.Errorf("Unexpected info: %+v", hs.Info)
	}
	if hs.Net.FileTranTCPPort != 13603 {
		t.Errorf("Expected the original client's network-order port 13603, got %d", hs.Net.FileTranTCPPort)
	}
	for _, element := range []string{
		"<ACTIVE_SEND>1</ACTIVE_SEND>",
		"<SYS_FACE>401</SYS_FACE>",
		"<FOLDER_TRAN>2:1</FOLDER_TRAN>",
		"<QGROUP>2:1</QGROUP>",
		"<LAN_UPDATE>1</LAN_UPDATE>",
		"<HIDE_RECORD>1</HIDE_RECORD>",
		"<FILE_TRAN_TCP_REVERSE_PORT>",
		"<FOLDER_TRAN_TCP_PORT>",
	} {
		if !bytes.Contains(formatted, []byte(element)) {
			t.Errorf("native handshake is missing %s:\n%s", element, formatted)
		}
	}
}

func TestHandsharkAdvertisesReverseTCPPort(t *testing.T) {
	const reversePort = 62620
	formatted := formatProfileWithReverse(wireProfile{Name: "Receiver", Group: "Default"}, 9013, reversePort, 0, false)
	hs, err := ParseHandshark(formatted)
	if err != nil {
		t.Fatal(err)
	}
	if bits.ReverseBytes16(hs.Net.FileTranTCPReversePort) != reversePort {
		t.Fatalf("reverse port = %d, want %d", bits.ReverseBytes16(hs.Net.FileTranTCPReversePort), reversePort)
	}
	if hs.Net.FileTranTCPPort != bits.ReverseBytes16(9013) {
		t.Fatalf("normal port = %d, want native network-order value", hs.Net.FileTranTCPPort)
	}
}

func TestHandsharkUsesDefaultWhenGroupIsEmpty(t *testing.T) {
	formatted := FormatHandshark("测试用户", "", "", 0, 9013, 9012, 0, "")
	hs, err := ParseHandshark(formatted)
	if err != nil {
		t.Fatal(err)
	}
	if hs.Info.Group != "Default" {
		t.Fatalf("empty group must be exposed as Default, got %q", hs.Info.Group)
	}
}
