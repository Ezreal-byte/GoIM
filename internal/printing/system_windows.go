//go:build windows

package printing

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"goim/internal/domain"
)

//go:embed windows_print.ps1
var windowsPrintHelper string

type windowsPrinterJSON struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Available    bool     `json:"available"`
	Default      bool     `json:"default"`
	Status       string   `json:"status"`
	Papers       []string `json:"papers"`
	Colors       []string `json:"colors"`
	Duplex       []string `json:"duplex"`
	Orientations []string `json:"orientations"`
}

func (System) List(ctx context.Context) ([]Printer, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	output, err := runPowerShellEnv(ctx, append(os.Environ(), "GOIM_PRINT_MODE=list"))
	if err != nil {
		return nil, fmt.Errorf("无法读取系统打印机：%w", err)
	}
	output = []byte(strings.TrimSpace(string(output)))
	if len(output) == 0 || string(output) == "null" {
		return nil, nil
	}
	var rows []windowsPrinterJSON
	if output[0] == '{' {
		var single windowsPrinterJSON
		if err := json.Unmarshal(output, &single); err != nil {
			return nil, err
		}
		rows = []windowsPrinterJSON{single}
	} else if err := json.Unmarshal(output, &rows); err != nil {
		return nil, err
	}
	printers := make([]Printer, 0, len(rows))
	for _, row := range rows {
		if strings.TrimSpace(row.ID) == "" {
			continue
		}
		printers = append(printers, Printer{
			ID: row.ID, Name: row.Name, Available: row.Available, Default: row.Default, Status: row.Status,
			Capabilities: NormalizeCapabilities(domain.PrintCapabilities{
				Papers: row.Papers, Orientations: row.Orientations,
				ColorModes: row.Colors, DuplexModes: row.Duplex, SupportsPageRange: true,
			}),
		})
	}
	return printers, nil
}

func (System) Submit(ctx context.Context, printer, path string, options domain.PrintOptions) (Receipt, error) {
	path, err := ValidateFile(path)
	if err != nil {
		return Receipt{}, err
	}
	options, err = NormalizeOptions(options, DefaultCapabilities())
	if err != nil {
		return Receipt{}, err
	}
	env := append(os.Environ(),
		"GOIM_PRINT_MODE=submit",
		"GOIM_PRINT_PRINTER="+printer,
		"GOIM_PRINT_FILE="+path,
		"GOIM_PRINT_COPIES="+strconv.Itoa(options.Copies),
		"GOIM_PRINT_PAPER="+options.Paper,
		"GOIM_PRINT_ORIENTATION="+options.Orientation,
		"GOIM_PRINT_COLOR="+options.Color,
		"GOIM_PRINT_DUPLEX="+options.Duplex,
		"GOIM_PRINT_RANGE="+options.PageRange,
	)
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	output, err := runPowerShellEnv(ctx, env)
	if err != nil {
		return Receipt{}, fmt.Errorf("无法提交系统打印队列：%w", err)
	}
	jobID := strings.TrimSpace(string(output))
	if jobID == "" {
		return Receipt{}, errors.New("系统打印队列未返回任务编号")
	}
	return Receipt{Printer: printer, JobID: jobID}, nil
}

func (System) Status(ctx context.Context, receipt Receipt) (State, error) {
	jobID, err := strconv.Atoi(strings.TrimSpace(receipt.JobID))
	if err != nil || jobID <= 0 || strings.TrimSpace(receipt.Printer) == "" {
		return StateUnknown, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	output, err := runPowerShellEnv(ctx, append(os.Environ(),
		"GOIM_PRINT_MODE=status",
		"GOIM_PRINT_PRINTER="+receipt.Printer,
		"GOIM_PRINT_JOB="+receipt.JobID,
	))
	if err != nil {
		return StateUnknown, fmt.Errorf("无法读取打印任务状态：%w", err)
	}
	switch strings.TrimSpace(string(output)) {
	case "queued":
		return StateQueued, nil
	case "completed":
		return StateCompleted, nil
	case "failed":
		return StateFailed, nil
	default:
		return StateUnknown, nil
	}
}

var runPowerShellEnv = func(ctx context.Context, env []string) ([]byte, error) {
	dir, err := os.MkdirTemp("", "goim-print-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "print.ps1")
	helper := append([]byte{0xef, 0xbb, 0xbf}, []byte(windowsPrintHelper)...)
	if err := os.WriteFile(path, helper, 0600); err != nil {
		return nil, err
	}
	command := exec.CommandContext(ctx, "powershell.exe", "-NoLogo", "-NoProfile", "-NonInteractive",
		"-ExecutionPolicy", "Bypass", "-File", path)
	command.Env = env
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	output, err := command.CombinedOutput()
	if err != nil {
		detail := strings.TrimSpace(string(output))
		if detail == "" {
			detail = err.Error()
		}
		return nil, errors.New(detail)
	}
	return output, nil
}
