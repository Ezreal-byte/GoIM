// Package buildinfo exposes the build facts compiled into GoIM.
package buildinfo

import (
	"net/url"
	"runtime"
	"runtime/debug"
	"strings"
	"sync"
	"time"
)

const (
	RepositoryURL = "https://github.com/Ezreal-byte/GoIM"
	IssuesURL     = RepositoryURL + "/issues"

	SourceLocal         = "local"
	SourceGitHubActions = "github-actions"
)

// The build scripts fill these strings with -ldflags -X. They stay plain
// strings so packaging needs no generated source or runtime process.
var (
	Version    = "0.1.0-dev"
	Commit     = ""
	CommitTime = ""
	Source     = SourceLocal
	BuildTime  = ""
	// UpdatePublicKey is injected into packaged releases. A development build
	// with no key never checks or installs network updates.
	UpdatePublicKey = ""
	CIRef           = ""
	CIRunID         = ""
	CIURL           = ""
)

// Info is the complete, read-only description of this executable.
type Info struct {
	Version     string
	Platform    string
	Source      string
	Revision    string
	CommitTime  string
	GoVersion   string
	VivaVersion string
	BuildTime   string
	CIRef       string
	CIRunID     string
	CIURL       string
}

// Injection is the set of facts supplied by a build script.
type Injection struct {
	Version    string
	Commit     string
	CommitTime string
	Source     string
	BuildTime  string
	CIRef      string
	CIRunID    string
	CIURL      string
}

var current = sync.OnceValue(func() Info {
	build, _ := debug.ReadBuildInfo()
	return Read(build, Injection{
		Version:    Version,
		Commit:     Commit,
		CommitTime: CommitTime,
		Source:     Source,
		BuildTime:  BuildTime,
		CIRef:      CIRef,
		CIRunID:    CIRunID,
		CIURL:      CIURL,
	})
})

// Current returns the runtime build facts once. Paint paths only read its
// immutable result.
func Current() Info { return current() }

// Read merges Go's read-only build record with facts supplied by the linker.
// vcs.time is the commit date; it is never presented as the build time.
func Read(build *debug.BuildInfo, injected Injection) Info {
	info := Info{
		Version:   clean(injected.Version),
		Platform:  Platform(runtime.GOOS, runtime.GOARCH),
		GoVersion: runtime.Version(),
		Source:    clean(injected.Source),
		CIRef:     clean(injected.CIRef),
		CIRunID:   clean(injected.CIRunID),
		CIURL:     safeURL(injected.CIURL),
	}
	var revision, revisionTime, framework string
	var modified bool
	if build != nil {
		if build.GoVersion != "" {
			info.GoVersion = build.GoVersion
		}
		for _, setting := range build.Settings {
			switch setting.Key {
			case "vcs.revision":
				revision = clean(setting.Value)
			case "vcs.time":
				revisionTime = clean(setting.Value)
			case "vcs.modified":
				modified = setting.Value == "true"
			}
		}
		for _, dependency := range build.Deps {
			if dependency.Path != "github.com/vivaui/viva" {
				continue
			}
			switch {
			case dependency.Replace != nil && dependency.Replace.Version != "":
				framework = clean(dependency.Replace.Version)
			case dependency.Replace != nil:
				framework = "local"
			case dependency.Version == "(devel)":
				framework = "local"
			default:
				framework = clean(dependency.Version)
			}
			break
		}
	}
	if commit := clean(injected.Commit); commit != "" {
		revision = commit
	} else if revision != "" && modified {
		revision += "-dirty"
	}
	if stamp := clean(injected.CommitTime); stamp != "" {
		revisionTime = stamp
	}
	if info.Version == "" {
		info.Version = "0.1.0-dev"
	}
	if info.Source == "" {
		info.Source = SourceLocal
	}
	info.Revision = revision
	info.CommitTime = date(revisionTime)
	info.VivaVersion = framework
	info.BuildTime = timestamp(injected.BuildTime)
	return info
}

// Platform names the operating system and architecture as a person reads them.
func Platform(goos, goarch string) string {
	system := goos
	switch goos {
	case "darwin":
		system = "macOS"
	case "windows":
		system = "Windows"
	case "linux":
		system = "Linux"
	case "freebsd":
		system = "FreeBSD"
	}
	switch goarch {
	case "amd64":
		goarch = "x86-64"
	case "386":
		goarch = "x86"
	case "arm64":
		goarch = "ARM64"
	}
	if system == "" {
		return goarch
	}
	if goarch == "" {
		return system
	}
	return system + " · " + goarch
}

func date(raw string) string {
	stamp, err := time.Parse(time.RFC3339, clean(raw))
	if err != nil {
		return ""
	}
	return stamp.UTC().Format("2006-01-02")
}

func timestamp(raw string) string {
	stamp, err := time.Parse(time.RFC3339, clean(raw))
	if err != nil {
		return ""
	}
	return stamp.UTC().Format("2006-01-02 15:04 UTC")
}

func safeURL(raw string) string {
	raw = clean(raw)
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "https" && parsed.Scheme != "http") {
		return ""
	}
	return parsed.String()
}

func clean(value string) string {
	return strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(value, "\r", ""), "\n", ""))
}
