package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// Run with GOIM_TEST_DIALOG_RUNTIME and a private xvfb-run display. The helper
// gets no desktop portal and no system dialog or library commands on PATH.
func TestBundledLinuxDialogOffline(t *testing.T) {
	runtimeDir := os.Getenv("GOIM_TEST_DIALOG_RUNTIME")
	if runtimeDir == "" {
		t.Skip("set GOIM_TEST_DIALOG_RUNTIME under xvfb-run")
	}
	t.Setenv("LC_ALL", "C.UTF-8")
	xdotool, err := exec.LookPath("xdotool")
	if err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(t.TempDir(), "bin")
	if err := os.MkdirAll(bin, 0755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"dirname", "mktemp", "rm"} {
		path, err := exec.LookPath(name)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(path, filepath.Join(bin, name)); err != nil {
			t.Fatal(err)
		}
	}
	directory := filepath.Join(t.TempDir(), "中文 空格 & 目录")
	if err := os.MkdirAll(directory, 0755); err != nil {
		t.Fatal(err)
	}
	selected := filepath.Join(directory, "头像 图片.png")
	if err := os.WriteFile(selected, []byte("file selection fixture"), 0644); err != nil {
		t.Fatal(err)
	}
	for index, test := range []struct {
		name, path string
		args       []string
		cancel     bool
	}{
		{"open", selected, nil, false},
		{"multiple", selected, []string{"--multiple", "--separator=|"}, false},
		{"directory", directory, []string{"--directory"}, false},
		{"save", filepath.Join(directory, "保存 文件.txt"), []string{"--save", "--confirm-overwrite"}, false},
		{"cancel", "", nil, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			title := fmt.Sprintf("GoIM offline QA %d", index)
			args := append([]string{"--file-selection", "--title=" + title}, test.args...)
			if test.path != "" {
				filename := test.path
				if test.name == "directory" {
					filename += "/"
				}
				args = append(args, "--filename="+filename)
			}
			command := exec.CommandContext(ctx, filepath.Join(runtimeDir, "bin", "zenity"), args...)
			command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
			command.WaitDelay = time.Second
			started := time.Now()
			var stdout, stderr bytes.Buffer
			command.Stdout, command.Stderr = &stdout, &stderr
			command.Env = append(os.Environ(), "PATH="+bin, "DBUS_SESSION_BUS_ADDRESS=unix:path=/nonexistent-goim-qa-bus", "LANG=C.UTF-8", "LC_ALL=C.UTF-8", "GDK_BACKEND=x11")
			if err := command.Start(); err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() { done <- command.Wait() }()
			defer func() { cancel(); _ = syscall.Kill(-command.Process.Pid, syscall.SIGKILL) }()
			window, err := exec.CommandContext(ctx, xdotool, "search", "--sync", "--onlyvisible", "--name", "^"+title+"$").Output()
			if err != nil {
				t.Fatalf("dialog did not open: %v", err)
			}
			id := strings.Fields(string(window))[0]
			assertPrivateDialogLibraries(t, command.Process.Pid, runtimeDir, time.Since(started))
			key := func(args ...string) {
				result, err := exec.CommandContext(ctx, xdotool, args...).CombinedOutput()
				if err != nil {
					t.Fatalf("xdotool %v: %v %s", args, err, result)
				}
			}
			key("windowfocus", "--sync", id)
			if test.cancel {
				key("key", "Escape")
			} else {
				// GTK loads the directory asynchronously. Accept once its selected
				// filename becomes available, without depending on a fixed delay.
				for {
					select {
					case err = <-done:
						goto finished
					case <-time.After(100 * time.Millisecond):
						if ctx.Err() == nil {
							key("key", "alt+o")
						}
					}
				}
			}
			err = <-done
		finished:
			if test.cancel {
				if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 1 || stdout.Len() != 0 {
					t.Fatalf("cancel: %v %s %s", err, &stdout, &stderr)
				}
			} else if err != nil || strings.TrimSpace(stdout.String()) != test.path {
				t.Fatalf("selection: %v got=%q want=%q stderr=%s", err, &stdout, test.path, &stderr)
			}
		})
	}
}

func assertPrivateDialogLibraries(t *testing.T, wrapperPID int, root string, startup time.Duration) {
	t.Helper()
	children, err := os.ReadFile(fmt.Sprintf("/proc/%d/task/%d/children", wrapperPID, wrapperPID))
	if err != nil {
		t.Fatal(err)
	}
	pids := strings.Fields(string(children))
	if len(pids) != 1 {
		t.Fatalf("dialog children: %s", children)
	}
	maps, err := os.ReadFile("/proc/" + pids[0] + "/maps")
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(maps), "\n") {
		if strings.Contains(line, ".so") && !strings.Contains(line, root+"/") {
			t.Fatalf("dialog loaded a host library: %s", line)
		}
	}
	status, err := os.ReadFile("/proc/" + pids[0] + "/status")
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(status), "\n") {
		if strings.HasPrefix(line, "VmRSS:") {
			t.Logf("dialog visible in %s; %s; all mapped libraries private", startup.Round(time.Millisecond), line)
		}
	}
}
