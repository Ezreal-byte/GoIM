package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vivaui/viva/tools/winpkg"
)

func TestWindowsSetupRequiresExitBeforeReplacement(t *testing.T) {
	source, err := (winpkg.Script{Name: "GoIM", Version: "0.1.14", Publisher: "GoIM", ID: "dev.goim.desktop",
		Executable: "GoIM.exe", Program: "fixture.exe", Icon: "fixture.ico", Arch: "amd64", Output: "fixture-Setup.exe"}).Generate()
	if err != nil {
		t.Fatal(err)
	}
	guarded, err := requireClosedGoIM(source)
	if err != nil {
		t.Fatal(err)
	}
	text := string(guarded)
	start := strings.Index(text, "Function ${un}EnsureClosed\n")
	end := strings.Index(text[start:], "FunctionEnd\n")
	guard := text[start : start+end]
	for _, required := range []string{`FindWindow $R0 "VivaNativeWindow" "GoIM"`, "MB_RETRYCANCEL", "/SD IDCANCEL", "SetErrorLevel 1618", "Abort"} {
		if !strings.Contains(guard, required) {
			t.Fatalf("installer close guard is missing %q", required)
		}
	}
	if strings.Contains(guard, "Rename ") {
		t.Fatal("installer may not rename a running application aside")
	}
	if _, err := requireClosedGoIM([]byte("unrecognized upstream installer")); err == nil {
		t.Fatal("upstream installer changes silently bypassed the running-app guard")
	}
}

func TestWindowsSetupCompilerQA(t *testing.T) {
	if os.Getenv("GOIM_SETUP_COMPILER_QA") != "1" {
		t.Skip("set GOIM_SETUP_COMPILER_QA=1 with NSIS available")
	}
	t.Chdir(filepath.Join("..", ".."))
	directory := t.TempDir()
	payload := filepath.Join(directory, "payload")
	if err := os.Mkdir(payload, 0o700); err != nil {
		t.Fatal(err)
	}
	icon, err := os.ReadFile(filepath.Join("internal", "brand", "assets", "goim.ico"))
	if err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string][]byte{"GoIM.exe": []byte("compiler fixture"), "GoIM.ico": icon, "LICENSE": []byte("fixture")} {
		if err := os.WriteFile(filepath.Join(payload, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, architecture := range []string{"amd64", "arm64"} {
		output := filepath.Join(directory, architecture+"-Setup.exe")
		if err := buildWindowsSetup(payload, output, "0.1.14", architecture); err != nil {
			t.Fatal(err)
		}
		if info, err := os.Stat(output); err != nil || info.Size() == 0 {
			t.Fatalf("empty installer: %v", err)
		}
	}
}
