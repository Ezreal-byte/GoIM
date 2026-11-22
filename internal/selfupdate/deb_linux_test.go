package selfupdate

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestDebRelaunchRequiresHandoffAndExit(t *testing.T) {
	for _, test := range []struct {
		name, pid, authorization string
		ready, relaunched        bool
	}{
		{"cancel", "99999999", "exit 126", false, false},
		{"still running", strconv.Itoa(os.Getpid()), "echo ready", true, false},
		{"restart", "99999999", "echo ready", true, true},
		{"installer failed after exit", "99999999", "echo ready; exit 1", true, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			for name, body := range map[string]string{"authorize": test.authorization, "app": `printf '%s' "$1" > "$RESULT"`} {
				if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"+body+"\n"), 0700); err != nil {
					t.Fatal(err)
				}
			}
			script := strings.NewReplacer("/usr/bin/pkexec", `"$AUTHORIZE"`, "/usr/bin/GoIM", `"$APP"`).Replace(debRelaunch)
			const arg = "--data-dir=/a b/$literal"
			cmd := exec.Command("/bin/sh", "-c", script, "test", test.pid, "helper", "receipt", arg)
			marker := filepath.Join(dir, "result")
			cmd.Env = append(os.Environ(), "RESULT="+marker, "AUTHORIZE="+filepath.Join(dir, "authorize"), "APP="+filepath.Join(dir, "app"))
			out, err := cmd.Output()
			if (string(out) == "ready\n") != test.ready || (err == nil) != test.relaunched {
				t.Fatalf("handoff %q, result %v", out, err)
			}
			got, err := os.ReadFile(marker)
			if test.relaunched && (err != nil || string(got) != arg) || !test.relaunched && !os.IsNotExist(err) {
				t.Fatalf("relaunch %q, error %v", got, err)
			}
		})
	}
}
