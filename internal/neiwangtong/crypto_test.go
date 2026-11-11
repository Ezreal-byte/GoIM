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
	if len(enc)%8 != 0 {
		t.Fatalf("Padded length should be multiple of 8, got %d", len(enc))
	}
	dec := make([]byte, len(enc))
	if err := Decrypt(dec, enc, Key); err != nil {
		t.Fatalf("Decrypt error: %v", err)
	}
	if !strings.HasPrefix(string(dec), string(plain)) {
		t.Fatalf("Decrypted does not match plain: %s", string(dec))
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
