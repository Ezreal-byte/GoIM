// Package diagnostics builds privacy-safe support reports for GoIM.
package diagnostics

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"goim/internal/buildinfo"
	"goim/internal/domain"
)

// Input is the immutable application snapshot used to build a report. The
// collector runs after painting, so filesystem and network-interface probes
// never execute from a Viva paint callback.
type Input struct {
	Now                    time.Time
	Build                  buildinfo.Info
	State                  domain.State
	Autostart              string
	InterfaceFontSupported bool
	DataDir                string
	Interfaces             func() ([]net.Interface, error)
}

var (
	urlPattern     = regexp.MustCompile(`(?i)\b(?:https?|ftp)://[^\s]+`)
	emailPattern   = regexp.MustCompile(`(?i)\b[A-Z0-9._%+\-]+@[A-Z0-9.\-]+\.[A-Z]{2,}\b`)
	ipv4Pattern    = regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}\b`)
	ipv6Pattern    = regexp.MustCompile(`(?i)\b(?:[0-9a-f]{0,4}:){2,}[0-9a-f:.]+\b`)
	pathPattern    = regexp.MustCompile(`(?:[A-Za-z]:[\\/]|/)[^\s,;]+`)
	secretPattern  = regexp.MustCompile(`(?i)\b(password|passwd|token|secret|authorization|api[_-]?key)\s*[:=]\s*\S+`)
	controlPattern = regexp.MustCompile(`[\x00-\x08\x0b\x0c\x0e-\x1f\x7f]`)
)

// Build returns a deterministic, plain-text support report. It deliberately
// excludes message bodies, contact names, addresses, credentials, URLs, file
// names and filesystem paths.
func Build(input Input) string {
	now := input.Now
	if now.IsZero() {
		now = time.Now()
	}
	state := input.State
	var out strings.Builder
	line := func(format string, args ...any) {
		fmt.Fprintf(&out, format+"\n", args...)
	}
	fact := func(label, value string) {
		line("%s: %s", label, reportValue(value))
	}
	section := func(title string) {
		line("\n== %s ==", title)
	}

	line("== GoIM diagnostics ==")
	fact("created", now.UTC().Format(time.RFC3339))
	fact("version", state.Version)
	fact("build version", input.Build.Version)
	fact("platform", input.Build.Platform)
	fact("runtime", runtime.Version())
	fact("viva", input.Build.VivaVersion)
	fact("source", input.Build.Source)
	fact("revision", input.Build.Revision)
	fact("commit time", input.Build.CommitTime)
	fact("build time", input.Build.BuildTime)
	fact("cpu count", strconv.Itoa(runtime.NumCPU()))

	section("Settings and lifecycle")
	settings := state.Settings
	fact("language", settings.Language)
	fact("theme", settings.Theme)
	fact("interface font", valueWhen(strings.TrimSpace(settings.InterfaceFont) != "", "custom", "system"))
	fact("interface font supported", strconv.FormatBool(input.InterfaceFontSupported))
	fact("launch at login", valueWhen(input.Autostart != "", input.Autostart, "unknown"))
	fact("close to tray", strconv.FormatBool(settings.CloseToTray))
	fact("desktop notifications", strconv.FormatBool(settings.DesktopNotifications))
	fact("notification preview", valueWhen(settings.HideNotificationText, "hidden", "shown"))
	fact("auto discovery", valueWhen(settings.DisableAutoDiscovery, "disabled", "enabled"))
	fact("custom background", valueWhen(strings.TrimSpace(settings.Background) != "", "configured", "none"))
	fact("app lock", valueWhen(settings.HasLockPassword, "configured", "not configured"))
	fact("system unlock", valueWhen(settings.SystemUnlock, "enabled", "disabled"))

	section("Protocol services")
	fact("listener", listenerSummary(state.ListenAddress))
	fact("goim peers", strconv.Itoa(len(state.Peers)))
	fact("neiwangtong", compatibilitySummary(settings.Neiwangtong, state.NeiwangtongStatus))
	fact("ipmsg", compatibilitySummary(settings.IPMSG, state.IPMSGStatus))
	fact("network selection", valueWhen(strings.TrimSpace(settings.Network) != "", "adapter selected", "automatic"))
	fact("pending network change", valueWhen(strings.TrimSpace(settings.PendingNetwork) != "", "pending", "none"))

	section("System adapters")
	listInterfaces := input.Interfaces
	if listInterfaces == nil {
		listInterfaces = net.Interfaces
	}
	interfaces, err := listInterfaces()
	if err != nil {
		fact("enumeration", "failed: "+safeError(err.Error(), state))
	} else {
		fact("count", strconv.Itoa(len(interfaces)))
		for index, iface := range interfaces {
			name := sanitize(iface.Name, state)
			line("adapter %d: name=%s state=%s loopback=%t point-to-point=%t",
				index+1, name, interfaceState(iface.Flags), iface.Flags&net.FlagLoopback != 0,
				iface.Flags&net.FlagPointToPoint != 0)
		}
	}

	section("Counts")
	var online, direct, groups, unread, pendingInvites int
	for _, peer := range state.Peers {
		if peer.VisibleOnline() {
			online++
		}
	}
	for _, conversation := range state.Conversations {
		switch conversation.Kind {
		case "group":
			groups++
		default:
			direct++
		}
		unread += conversation.Unread
	}
	for _, invitation := range state.Invitations {
		if invitation.Status == "pending" {
			pendingInvites++
		}
	}
	messageKinds := make(map[string]int)
	for _, message := range state.Messages {
		messageKinds[message.Kind]++
	}
	transferStates := make(map[string]int)
	for _, transfer := range state.Transfers {
		transferStates[transfer.Status]++
	}
	var inlineImages, folders, shared int
	for _, file := range state.Files {
		if file.InlineImage {
			inlineImages++
		}
		if file.Folder {
			folders++
		}
		if file.Shared {
			shared++
		}
	}
	fact("peers", fmt.Sprintf("total=%d online=%d", len(state.Peers), online))
	fact("conversations", fmt.Sprintf("direct=%d group=%d unread=%d", direct, groups, unread))
	fact("messages", fmt.Sprintf("total=%d %s", len(state.Messages), countSummary(messageKinds)))
	fact("groups", strconv.Itoa(len(state.Groups)))
	fact("invitations", fmt.Sprintf("total=%d pending=%d", len(state.Invitations), pendingInvites))
	fact("files", fmt.Sprintf("total=%d inline-images=%d native-folders=%d shared=%d",
		len(state.Files), inlineImages, folders, shared))
	fact("transfers", fmt.Sprintf("total=%d %s", len(state.Transfers), countSummary(transferStates)))

	section("Storage and state")
	fact("backend snapshot", "readable")
	fact("settings store", "loaded")
	fact("database", "loaded by running backend")
	fact("download directory", directoryState(settings.DownloadDir))
	fact("application data", directoryState(input.DataDir))
	if strings.TrimSpace(input.DataDir) != "" {
		database := filepath.Join(input.DataDir, "goim.db")
		fact("database file", databaseFileState(database))
	}

	section("Recent error")
	if strings.TrimSpace(state.Error) == "" {
		line("none")
	} else {
		line("kind: %s", errorKind(state.Error))
		line("summary: %s", safeError(state.Error, state))
	}

	section("Privacy")
	line("Excluded: message text, contact names, account identifiers, IP addresses, URLs,")
	line("file names, filesystem paths, passwords, tokens and cryptographic material.")
	line("The report is generated locally and is not uploaded automatically.")
	return out.String()
}

func reportValue(value string) string {
	value = controlPattern.ReplaceAllString(strings.TrimSpace(value), "")
	if value == "" {
		return "not provided"
	}
	return value
}

func valueWhen(condition bool, yes, no string) string {
	if condition {
		return yes
	}
	return no
}

func compatibilitySummary(config domain.CompatibilityConfig, status domain.CompatibilityStatus) string {
	state := valueWhen(config.Enabled, "enabled", "disabled")
	if status.Unavailable {
		state = "unavailable"
	} else if status.Active {
		state += ", active"
	} else if config.Enabled {
		state += ", inactive"
	}
	if status.Pending != nil {
		state += ", change pending"
	}
	if strings.TrimSpace(status.Problem) != "" {
		state += ", problem reported"
	}
	return fmt.Sprintf("%s, port=%d", state, config.Port)
}

func listenerSummary(address string) string {
	_, port, err := net.SplitHostPort(strings.TrimSpace(address))
	if err != nil || port == "" {
		if strings.TrimSpace(address) == "" {
			return "not provided"
		}
		return "configured (host redacted)"
	}
	return "tcp port " + port + " (host redacted)"
}

func interfaceState(flags net.Flags) string {
	if flags&net.FlagUp == 0 {
		return "down"
	}
	return "up"
}

func countSummary(values map[string]int) string {
	if len(values) == 0 {
		return "by-kind=none"
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		label := strings.TrimSpace(key)
		if label == "" {
			label = "unknown"
		}
		parts = append(parts, fmt.Sprintf("%s=%d", label, values[key]))
	}
	return "by-kind=" + strings.Join(parts, ",")
}

func directoryState(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return "not configured"
	}
	info, err := os.Stat(path)
	switch {
	case err != nil:
		return "unavailable"
	case !info.IsDir():
		return "not a directory"
	default:
		probe, createErr := os.CreateTemp(path, ".goim-diagnostics-*")
		if createErr != nil {
			return "present, not writable"
		}
		probePath := probe.Name()
		_, writeErr := probe.WriteString("probe")
		if writeErr == nil {
			writeErr = probe.Sync()
		}
		closeErr := probe.Close()
		removeErr := os.Remove(probePath)
		if writeErr != nil || closeErr != nil || removeErr != nil {
			return "present, not writable"
		}
		return "present, writable"
	}
}

func databaseFileState(path string) string {
	info, err := os.Stat(path)
	if err != nil {
		return "unavailable"
	}
	if !info.Mode().IsRegular() {
		return "not a regular file"
	}
	if info.Size() == 0 {
		return "present, empty"
	}
	file, err := os.Open(path)
	if err != nil {
		return "present, unreadable"
	}
	var probe [1]byte
	_, readErr := file.Read(probe[:])
	closeErr := file.Close()
	if readErr != nil || closeErr != nil {
		return "present, unreadable"
	}
	return "present, readable"
}

func errorKind(text string) string {
	value := strings.ToLower(text)
	switch {
	case containsAny(value, "database", "bbolt", "数据库"):
		return "database"
	case containsAny(value, "permission", "access is denied", "密码", "password", "auth"):
		return "permission-or-authentication"
	case containsAny(value, "timeout", "deadline exceeded", "超时"):
		return "timeout"
	case containsAny(value, "connection", "network", "socket", "连接", "网络"):
		return "network"
	case containsAny(value, "file", "directory", "folder", "文件", "目录"):
		return "file-system"
	case containsAny(value, "protocol", "handshake", "frame", "协议", "握手"):
		return "protocol"
	default:
		return "other"
	}
}

func containsAny(value string, candidates ...string) bool {
	for _, candidate := range candidates {
		if strings.Contains(value, candidate) {
			return true
		}
	}
	return false
}

func safeError(text string, state domain.State) string {
	redactions := sensitiveValues(state)
	value := strings.TrimSpace(text)
	for _, private := range redactions {
		if private == "" {
			continue
		}
		value = strings.ReplaceAll(value, private, "[redacted]")
		value = strings.ReplaceAll(value, strings.ToLower(private), "[redacted]")
	}
	value = urlPattern.ReplaceAllString(value, "[url]")
	value = emailPattern.ReplaceAllString(value, "[email]")
	value = ipv4Pattern.ReplaceAllString(value, "[ip]")
	value = ipv6Pattern.ReplaceAllString(value, "[ip]")
	value = pathPattern.ReplaceAllString(value, "[path]")
	value = secretPattern.ReplaceAllString(value, "$1=[redacted]")
	value = controlPattern.ReplaceAllString(value, "")
	value = strings.Join(strings.Fields(value), " ")
	if utf8.RuneCountInString(value) > 600 {
		value = string([]rune(value)[:600]) + "…"
	}
	if value == "" {
		return "unavailable"
	}
	return value
}

func sanitize(value string, state domain.State) string {
	value = safeError(value, state)
	if utf8.RuneCountInString(value) > 80 {
		value = string([]rune(value)[:80]) + "…"
	}
	return value
}

func sensitiveValues(state domain.State) []string {
	seen := make(map[string]bool)
	var values []string
	add := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			return
		}
		seen[value] = true
		values = append(values, value)
	}
	add(state.Self.ID)
	add(state.Self.Name)
	add(state.Self.Address)
	add(state.Self.Avatar)
	add(state.Self.Signature)
	add(state.Settings.Nickname)
	add(state.Settings.Avatar)
	add(state.Settings.Signature)
	add(state.Settings.Background)
	add(state.Settings.DownloadDir)
	for _, value := range []string{
		state.Settings.ProfileDetails.Company,
		state.Settings.ProfileDetails.Department,
		state.Settings.ProfileDetails.Phone,
		state.Settings.ProfileDetails.Mobile,
		state.Settings.ProfileDetails.Email,
	} {
		add(value)
	}
	for _, peer := range state.Peers {
		add(peer.ID)
		add(peer.Name)
		add(peer.Address)
		add(peer.Avatar)
		add(peer.Signature)
		add(peer.ContactGroup)
		add(peer.Details.Company)
		add(peer.Details.Department)
		add(peer.Details.Phone)
		add(peer.Details.Mobile)
		add(peer.Details.Email)
	}
	for _, conversation := range state.Conversations {
		add(conversation.ID)
		add(conversation.Title)
		add(conversation.Preview)
		add(conversation.Draft)
	}
	for _, message := range state.Messages {
		add(message.ID)
		add(message.Text)
		add(message.Nickname)
	}
	for _, group := range state.Groups {
		add(group.ID)
		add(group.Name)
	}
	for _, invitation := range state.Invitations {
		add(invitation.GroupName)
		add(invitation.FromName)
	}
	for _, file := range state.Files {
		add(file.Name)
		add(file.SourcePath)
		add(file.ShareURL)
	}
	for _, transfer := range state.Transfers {
		add(transfer.Name)
		add(transfer.Path)
	}
	sort.Slice(values, func(i, j int) bool {
		return len(values[i]) > len(values[j])
	})
	return values
}

// WritePrivateFile writes a report with owner-only permissions on platforms
// that support POSIX modes. It refuses empty destinations.
func WritePrivateFile(path, report string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return fmt.Errorf("diagnostics: empty destination")
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if err = file.Chmod(0o600); err == nil {
		_, err = file.WriteString(report)
	}
	if err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil {
		return err
	}
	return closeErr
}
