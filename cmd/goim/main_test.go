package main

import (
	"flag"
	"path/filepath"
	"testing"
)

func TestRestartRetainsOptionsAndResolvesDirectories(t *testing.T) {
	type options struct {
		data, downloads, name string
		noDiscovery           bool
	}
	parse := func(args []string) (*flag.FlagSet, options) {
		t.Helper()
		var o options
		f := flag.NewFlagSet("goim", flag.ContinueOnError)
		f.StringVar(&o.data, "data-dir", "", "")
		f.StringVar(&o.downloads, "download-dir", "", "")
		f.StringVar(&o.name, "name", "", "")
		f.BoolVar(&o.noDiscovery, "no-discovery", false, "")
		if err := f.Parse(args); err != nil {
			t.Fatal(err)
		}
		return f, o
	}
	original, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Chdir(original)
	f, _ := parse([]string{"--data-dir", "测试 数据", "--download-dir=received", "--name=GoIM & friends", "--no-discovery"})
	restart, err := restartArguments(f)
	if err != nil {
		t.Fatal(err)
	}
	t.Chdir(t.TempDir())
	_, got := parse(restart)
	want := options{
		data: filepath.Join(original, "测试 数据"), downloads: filepath.Join(original, "received"),
		name: "GoIM & friends", noDiscovery: true,
	}
	if got != want {
		t.Fatalf("relaunch options = %+v, want %+v", got, want)
	}
}
