package neiwangtong

import (
	"testing"
)

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

func TestHandsharkFormatAndParse(t *testing.T) {
	formatted := FormatHandshark("测试用户", "技术部", "在线中", 0, 9013, 9012, 123456, "abcdef123456")
	hs, err := ParseHandshark(formatted)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if hs.Info.Name != "测试用户" || hs.Info.Group != "技术部" || hs.Info.Sign != "在线中" {
		t.Errorf("Unexpected info: %+v", hs.Info)
	}
	if hs.Net.FileTranTCPPort != 9013 {
		t.Errorf("Expected port 9013, got %d", hs.Net.FileTranTCPPort)
	}
}
