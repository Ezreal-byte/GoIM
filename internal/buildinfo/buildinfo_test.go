package buildinfo

import (
	"runtime"
	"runtime/debug"
	"testing"
)

func TestReadKeepsCommitDateSeparateFromBuildTime(t *testing.T) {
	build := &debug.BuildInfo{
		GoVersion: "go1.27.3",
		Settings: []debug.BuildSetting{
			{Key: "vcs.revision", Value: "0123456789abcdef0123456789abcdef01234567"},
			{Key: "vcs.time", Value: "2026-11-07T08:09:10Z"},
		},
	}
	info := Read(build, Injection{Version: "1.2.3", Source: SourceGitHubActions})

	if info.Revision != "0123456789abcdef0123456789abcdef01234567" {
		t.Fatalf("revision = %q", info.Revision)
	}
	if info.CommitTime != "2026-11-07" {
		t.Fatalf("commit date = %q", info.CommitTime)
	}
	if info.BuildTime != "" {
		t.Fatalf("vcs.time leaked into build time: %q", info.BuildTime)
	}
	if info.GoVersion != "go1.27.3" || info.Platform != Platform(runtime.GOOS, runtime.GOARCH) {
		t.Fatalf("runtime facts = %+v", info)
	}
}

func TestReadUsesInjectedReleaseFacts(t *testing.T) {
	build := &debug.BuildInfo{
		Settings: []debug.BuildSetting{
			{Key: "vcs.revision", Value: "old"},
			{Key: "vcs.time", Value: "2026-01-01T00:00:00Z"},
		},
	}
	info := Read(build, Injection{
		Version:    "1.2.3",
		Commit:     "fedcba9876543210fedcba9876543210fedcba98",
		CommitTime: "2026-11-10T09:00:00Z",
		Source:     SourceGitHubActions,
		BuildTime:  "2026-11-10T12:34:56Z",
		CIRef:      "refs/tags/v1.2.3",
		CIRunID:    "12345",
		CIURL:      "https://github.com/Ezreal-byte/GoIM/actions/runs/12345",
	})

	if info.Version != "1.2.3" || info.Revision != "fedcba9876543210fedcba9876543210fedcba98" {
		t.Fatalf("release identity = %+v", info)
	}
	if info.CommitTime != "2026-11-10" {
		t.Fatalf("commit date = %q", info.CommitTime)
	}
	if info.BuildTime != "2026-11-10 12:34 UTC" {
		t.Fatalf("build time = %q", info.BuildTime)
	}
	if info.CIRef != "refs/tags/v1.2.3" || info.CIRunID != "12345" ||
		info.CIURL != "https://github.com/Ezreal-byte/GoIM/actions/runs/12345" {
		t.Fatalf("CI facts = %+v", info)
	}
}

func TestReadReportsTheLinkedFramework(t *testing.T) {
	for _, test := range []struct {
		name   string
		module debug.Module
		want   string
	}{
		{
			name:   "pinned module",
			module: debug.Module{Path: "github.com/vivaui/viva", Version: "v0.6.5-0.20261115140306-89ce846fe65c"},
			want:   "v0.6.5-0.20261115140306-89ce846fe65c",
		},
		{
			name:   "replacement without a version",
			module: debug.Module{Path: "github.com/vivaui/viva", Version: "v0.0.0", Replace: &debug.Module{Path: "../viva"}},
			want:   "local",
		},
		{
			name:   "devel module",
			module: debug.Module{Path: "github.com/vivaui/viva", Version: "(devel)"},
			want:   "local",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			info := Read(&debug.BuildInfo{Deps: []*debug.Module{&test.module}}, Injection{})
			if info.VivaVersion != test.want {
				t.Fatalf("Viva version = %q, want %q", info.VivaVersion, test.want)
			}
		})
	}
}

func TestReadRejectsUnknownAndUnsafeFacts(t *testing.T) {
	info := Read(nil, Injection{
		BuildTime: "not a timestamp",
		CIURL:     "file:///tmp/secret",
		Source:    "",
	})
	if info.BuildTime != "" || info.CIURL != "" {
		t.Fatalf("unsafe or unknown facts were accepted: %+v", info)
	}
	if info.Source != SourceLocal || info.Version == "" {
		t.Fatalf("defaults = %+v", info)
	}
}

func TestPlatformNamesKnownTargets(t *testing.T) {
	for _, test := range []struct {
		goos, goarch, want string
	}{
		{"windows", "amd64", "Windows · x86-64"},
		{"darwin", "arm64", "macOS · ARM64"},
		{"linux", "386", "Linux · x86"},
		{"plan9", "mips", "plan9 · mips"},
	} {
		if got := Platform(test.goos, test.goarch); got != test.want {
			t.Errorf("Platform(%q, %q) = %q, want %q", test.goos, test.goarch, got, test.want)
		}
	}
}
