package firewall

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/godbus/dbus/v5"
)

// Keep at most 1 MiB while still draining both pipes on hosts with large policies.
type commandOutput struct {
	buf       bytes.Buffer
	truncated bool
}

func (b *commandOutput) Write(p []byte) (int, error) {
	n := min(len(p), (1<<20)-b.buf.Len())
	_, _ = b.buf.Write(p[:n])
	b.truncated = b.truncated || n < len(p)
	return len(p), nil
}

func securityCenterHint() string {
	data, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		id, ok := strings.CutPrefix(line, "ID=")
		if !ok {
			continue
		}
		switch strings.ToLower(strings.Trim(id, "'\" \r")) {
		case "kylin":
			return "麒麟可在「安全中心 → 网络防护」核对网络类型，并在「联网控制」查看 GoIM 的应用权限。"
		case "uos":
			return "统信可在「安全中心」核对防火墙策略。使用集中域管的设备请管理员同步核对终端策略。"
		}
	}
	return ""
}

func systemTool(name string) string {
	for _, dir := range []string{"/usr/sbin", "/usr/bin", "/sbin", "/bin"} {
		path := filepath.Join(dir, name)
		if st, err := os.Stat(path); err == nil && st.Mode().IsRegular() && st.Mode().Perm()&0111 != 0 {
			return path
		}
	}
	return ""
}

func systemCommands() commands {
	return commands{has: func(tool string) bool { return systemTool(tool) != "" }, run: runTool, firewalldOwner: firewalldOwner}
}

// Asking the bus for the owner never activates an idle firewall daemon.
func firewalldOwner(ctx context.Context) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	conn, err := dbus.ConnectSystemBus(dbus.WithContext(ctx))
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer conn.Close()
	var active bool
	err = conn.BusObject().CallWithContext(ctx, "org.freedesktop.DBus.NameHasOwner", 0, "org.fedoraproject.FirewallD1").Store(&active)
	return active, err
}

func runTool(ctx context.Context, authorized bool, name string, args ...string) (string, error) {
	path := systemTool(name)
	if path == "" {
		return "", fmt.Errorf("%s: %w", name, os.ErrNotExist)
	}
	timeout := 5 * time.Second
	elevate := authorized && os.Geteuid() != 0
	if elevate {
		args = append([]string{"--disable-internal-agent", path}, args...)
		path = systemTool("pkexec")
		if path == "" {
			return "", fmt.Errorf("%w: pkexec: %w", ErrAuthorization, os.ErrNotExist)
		}
		timeout = 60 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, args...)
	// The backend and PolicyKit agent use system executables and English status tokens.
	cmd.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "LC_ALL=C", "LANG=C"}
	for _, key := range []string{"DISPLAY", "XAUTHORITY", "WAYLAND_DISPLAY", "XDG_RUNTIME_DIR", "DBUS_SESSION_BUS_ADDRESS"} {
		if v, ok := os.LookupEnv(key); ok {
			cmd.Env = append(cmd.Env, key+"="+v)
		}
	}
	var out commandOutput
	cmd.Stdout, cmd.Stderr = &out, &out
	err := cmd.Run()
	s := strings.TrimSpace(out.buf.String())
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	if out.truncated {
		return "", fmt.Errorf("%s: output exceeds 1 MiB", name)
	}
	if err != nil {
		if elevate && exitCode(err) == 126 {
			return s, ErrCancelled
		}
		if elevate && exitCode(err) == 127 {
			return s, fmt.Errorf("%w: pkexec: %.300s", ErrAuthorization, s)
		}
		return s, fmt.Errorf("%s: %w: %.300s", name, err, s)
	}
	return s, nil
}

func localLinks() ([]link, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	var links []link
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			return nil, err
		}
		for _, addr := range addrs {
			p, err := netip.ParsePrefix(addr.String())
			if err == nil && p.Addr().Is4() && !p.Addr().IsUnspecified() && !p.Addr().IsLoopback() && p.Bits() > 0 {
				links = append(links, link{iface.Name, p})
			}
		}
	}
	return links, nil
}

func Inspect(ctx context.Context, c Config) (Report, error) {
	links, err := localLinks()
	if err != nil {
		return Report{}, err
	}
	r, err := inspect(ctx, plan(c, links), systemCommands())
	r.Guidance = securityCenterHint()
	return r, err
}

func Apply(ctx context.Context, c Config, preview Report) (Report, error) {
	links, err := localLinks()
	if err != nil {
		return Report{}, err
	}
	r, err := apply(ctx, plan(c, links), preview, systemCommands())
	r.Guidance = securityCenterHint()
	return r, err
}
