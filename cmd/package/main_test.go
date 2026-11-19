package main

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMacUpdateZIPHasGoIMBundleAtRoot(t *testing.T) {
	directory := t.TempDir()
	staging := filepath.Join(directory, "GoIM_0.1.10_darwin_arm64")
	want := []string{
		"GoIM.app/Contents/Info.plist",
		"GoIM.app/Contents/MacOS/GoIM",
		"GoIM.app/Contents/Helpers/ShotletCapture.app/Contents/MacOS/ShotletCapture",
		"build-info.json",
		"licenses/shotlet/Apache-2.0.txt",
	}
	for _, name := range want {
		path := filepath.Join(staging, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(name), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	archive := staging + ".zip"
	if err := pack(staging, archive, "darwin"); err != nil {
		t.Fatal(err)
	}
	z, err := zip.OpenReader(archive)
	if err != nil {
		t.Fatal(err)
	}
	defer z.Close()
	entries := make(map[string]bool)
	for _, entry := range z.File {
		entries[entry.Name] = true
	}
	for _, name := range want {
		if !entries[name] {
			t.Errorf("update ZIP is missing %q at its expected path", name)
		}
	}
}

func TestMacBundleDeclaresMicrophoneUsage(t *testing.T) {
	info := darwinBundleInfo("0.1.12")
	for _, extra := range info.Extra {
		if extra.Key == "NSMicrophoneUsageDescription" && strings.Contains(extra.Value, "microphone") {
			return
		}
	}
	t.Fatalf("macOS bundle does not declare NSMicrophoneUsageDescription: %+v", info.Extra)
}

func TestPackageBuildFactsUsesTheRealGitHubRun(t *testing.T) {
	environment := map[string]string{
		"GITHUB_ACTIONS":    "true",
		"GITHUB_REF":        "refs/tags/v1.2.3",
		"GITHUB_SERVER_URL": "https://github.com",
		"GITHUB_REPOSITORY": "Ezreal-byte/GoIM",
		"GITHUB_RUN_ID":     "987654321",
	}
	now := time.Date(2026, 11, 10, 12, 34, 56, 0, time.FixedZone("CST", 8*60*60))
	facts := packageBuildFactsFromEnvironment(func(key string) string {
		return environment[key]
	}, now)

	if facts.source != "github-actions" {
		t.Fatalf("source = %q", facts.source)
	}
	if facts.ciRef != "refs/tags/v1.2.3" || facts.ciRunID != "987654321" {
		t.Fatalf("CI facts = %+v", facts)
	}
	if facts.ciURL != "https://github.com/Ezreal-byte/GoIM/actions/runs/987654321" {
		t.Fatalf("CI URL = %q", facts.ciURL)
	}
	if facts.buildTime != "2026-11-10T04:34:56Z" {
		t.Fatalf("build time = %q", facts.buildTime)
	}
	if strings.Contains(facts.ciURL, `\`) || strings.Contains(facts.ciURL, "C:") {
		t.Fatalf("CI URL contains a local path: %q", facts.ciURL)
	}
}

func TestPackageBuildFactsStayLocalOutsideCI(t *testing.T) {
	now := time.Date(2026, 11, 10, 12, 34, 56, 0, time.UTC)
	facts := packageBuildFactsFromEnvironment(func(string) string { return "" }, now)
	if facts.source != "local" || facts.ciRef != "" || facts.ciRunID != "" || facts.ciURL != "" {
		t.Fatalf("local facts = %+v", facts)
	}
	if facts.buildTime != "2026-11-10T12:34:56Z" {
		t.Fatalf("build time = %q", facts.buildTime)
	}
}

func TestPackageLinkerFlagsCarryBuildInfo(t *testing.T) {
	facts := packageBuildFacts{
		source:     "github-actions",
		ciRef:      "refs/tags/v1.2.3",
		ciRunID:    "987654321",
		ciURL:      "https://github.com/Ezreal-byte/GoIM/actions/runs/987654321",
		buildTime:  "2026-11-10T04:34:56Z",
		commitTime: "2026-11-09T12:00:00Z",
	}
	got := packageLinkerFlags(
		"1.2.3",
		"0123456789abcdef0123456789abcdef01234567",
		facts,
		true,
	)
	for _, want := range []string{
		"goim/internal/buildinfo.Commit=0123456789abcdef0123456789abcdef01234567",
		"goim/internal/buildinfo.Version=1.2.3",
		"goim/internal/buildinfo.Source=github-actions",
		"goim/internal/buildinfo.BuildTime=2026-11-10T04:34:56Z",
		"goim/internal/buildinfo.CIRef=refs/tags/v1.2.3",
		"goim/internal/buildinfo.CIRunID=987654321",
		"goim/internal/buildinfo.CIURL=https://github.com/Ezreal-byte/GoIM/actions/runs/987654321",
		"-H=windowsgui",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("linker flags do not contain %q", want)
		}
	}
	if strings.Contains(got, `C:\`) || strings.Contains(got, "USERPROFILE") {
		t.Fatalf("linker flags contain a local path: %s", got)
	}
}

func TestLinkerValuesRejectWhitespaceAndQuotes(t *testing.T) {
	for _, value := range []string{
		"",
		"https://github.com/Ezreal-byte/GoIM",
		"refs/tags/v1.2.3",
		"2026-11-10T04:34:56Z",
	} {
		if !validLinkerValue(value) {
			t.Errorf("validLinkerValue(%q) = false", value)
		}
	}
	for _, value := range []string{`C:\Users\me`, "two words", "line\nbreak", `quoted"value"`} {
		if validLinkerValue(value) {
			t.Errorf("validLinkerValue(%q) = true", value)
		}
	}
}

func TestWindowsInstallerVersion(t *testing.T) {
	for input, want := range map[string]string{
		"0.1.8":        "0.1.8",
		"0.1.8-beta.1": "0.1.8.1",
		"0.1.8-beta.2": "0.1.8.2",
		"1.2.3-rc.12":  "1.2.3.12",
	} {
		got, err := windowsInstallerVersion(input)
		if err != nil {
			t.Errorf("windowsInstallerVersion(%q): %v", input, err)
			continue
		}
		if got != want {
			t.Errorf("windowsInstallerVersion(%q) = %q, want %q", input, got, want)
		}
	}
	for _, input := range []string{
		"",
		"v0.1.8",
		"0.1.8.beta.1",
		"0.1.8-beta",
		"0.1.8-beta.0",
		"0.1.8-beta.70000",
		"1.2.3.4-beta.1",
	} {
		if got, err := windowsInstallerVersion(input); err == nil {
			t.Errorf("windowsInstallerVersion(%q) = %q, want error", input, got)
		}
	}
}
