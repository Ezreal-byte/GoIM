package neiwangtong

import (
	"encoding/hex"
	"strings"
	"testing"
)

func TestXTEARoundTrip(t *testing.T) {
	plain := []byte(`<X_SEND_MSG docver="1"><MSG_ID>123</MSG_ID><MSG>hello neiwangtong</MSG></X_SEND_MSG>`)
	enc, err := Encrypt(nil, plain, Key)
	if err != nil {
		t.Fatalf("Encrypt error: %v", err)
	}
	if len(enc) != len(plain) {
		t.Fatalf("ciphertext length changed from %d to %d", len(plain), len(enc))
	}
	dec := make([]byte, len(enc))
	if err := Decrypt(dec, enc, Key); err != nil {
		t.Fatalf("Decrypt error: %v", err)
	}
	if string(dec) != string(plain) {
		t.Fatalf("Decrypted does not match plain: %s", string(dec))
	}
}

func TestEncryptCapturedHeartbeat(t *testing.T) {
	plain := []byte(`<X_HEARTBEAT docver="1" />`)
	expected, err := hex.DecodeString("d529c5439de045c9d3dc11866769e1fedfa896e8e704a5532f3e")
	if err != nil {
		t.Fatal(err)
	}
	encrypted, err := Encrypt(nil, plain, Key)
	if err != nil {
		t.Fatal(err)
	}
	if string(encrypted) != string(expected) {
		t.Fatalf("captured heartbeat mismatch:\nwant %x\n got %x", expected, encrypted)
	}
}

func TestInvalidKey(t *testing.T) {
	if _, err := Encrypt(nil, []byte("test"), []byte("short")); err == nil {
		t.Fatal("Expected error for short key")
	}
	if err := Decrypt(make([]byte, 8), make([]byte, 8), []byte("short")); err == nil {
		t.Fatal("Expected error for short key")
	}
}

func TestDecryptCapturedStatus(t *testing.T) {
	rawHex := "36af741655c86a859229113f6883a31e3a643fa23c20e0aec1aaf895d8ce4ca98a324c97211460755ed24a2c453c12f209d3b6367313413618966a889a6e3490"
	cipher, err := hex.DecodeString(rawHex)
	if err != nil {
		t.Fatalf("Decode error: %v", err)
	}
	plain := make([]byte, len(cipher))
	if err := Decrypt(plain, cipher, Key); err != nil {
		t.Fatalf("Decrypt error: %v", err)
	}
	expected := `<X_CHANGE_STATUS docver="1"><STATUS>0</STATUS></X_CHANGE_STATUS>`
	if !strings.HasPrefix(string(plain), expected) {
		t.Fatalf("Expected prefix %q, got %q", expected, string(plain))
	}
}
