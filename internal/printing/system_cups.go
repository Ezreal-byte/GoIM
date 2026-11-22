//go:build linux || darwin

package printing

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"time"

	"goim/internal/domain"
)

func (System) List(ctx context.Context) ([]Printer, error) {
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	output, err := run(ctx, "lpstat", "-e")
	if err != nil {
		return nil, cupsError("无法读取系统打印机", output, err)
	}
	names := uniqueLines(string(output))
	defaultName := ""
	if output, err := run(ctx, "lpstat", "-d"); err == nil {
		if _, value, found := strings.Cut(strings.TrimSpace(string(output)), ":"); found {
			defaultName = strings.TrimSpace(value)
		}
	}
	printers := make([]Printer, 0, len(names))
	for _, name := range names {
		printer := Printer{Name: name, ID: name, Default: name == defaultName, Available: true,
			Capabilities: DefaultCapabilities()}
		if status, err := run(ctx, "lpstat", "-p", name); err == nil {
			text := strings.TrimSpace(string(status))
			printer.Status = text
			lower := strings.ToLower(text)
			printer.Available = !strings.Contains(lower, "disabled") && !strings.Contains(lower, "not accepting")
		}
		if options, err := run(ctx, "lpoptions", "-p", name, "-l"); err == nil {
			printer.Capabilities = cupsCapabilities(string(options), printer.Capabilities)
		}
		printers = append(printers, printer)
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
	args := []string{"-d", printer, "-n", strconv.Itoa(options.Copies), "-o", "media=" + options.Paper}
	orientation := "3"
	if options.Orientation == domain.PrintOrientationLandscape {
		orientation = "4"
	}
	args = append(args, "-o", "orientation-requested="+orientation)
	switch options.Color {
	case domain.PrintColorColor:
		args = append(args, "-o", "print-color-mode=color")
	case domain.PrintColorMonochrome:
		args = append(args, "-o", "print-color-mode=monochrome")
	}
	switch options.Duplex {
	case domain.PrintDuplexSimplex:
		args = append(args, "-o", "sides=one-sided")
	case domain.PrintDuplexLongEdge:
		args = append(args, "-o", "sides=two-sided-long-edge")
	case domain.PrintDuplexShortEdge:
		args = append(args, "-o", "sides=two-sided-short-edge")
	}
	if options.PageRange != "" {
		args = append(args, "-o", "page-ranges="+options.PageRange)
	}
	args = append(args, path)
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	output, err := run(ctx, "lp", args...)
	if err != nil {
		return Receipt{}, cupsError("无法提交系统打印队列", output, err)
	}
	fields := strings.Fields(string(output))
	jobID := ""
	for i, field := range fields {
		if field == "id" && i > 0 && fields[i-1] == "request" && i+2 < len(fields) {
			jobID = strings.TrimSuffix(fields[i+1], ",")
			break
		}
	}
	return Receipt{Printer: printer, JobID: jobID}, nil
}

// Cancel only the exact local request returned by lp.
func (System) Cancel(ctx context.Context, receipt Receipt) error {
	number, ok := strings.CutPrefix(receipt.JobID, receipt.Printer+"-")
	id, err := strconv.Atoi(number)
	if !ok || receipt.Printer == "" || strings.HasPrefix(receipt.Printer, "-") || err != nil || id <= 0 {
		return errors.New("系统打印任务编号无效")
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	out, err := run(ctx, "cancel", receipt.JobID)
	if err != nil {
		return cupsError("系统打印队列拒绝取消", out, err)
	}
	return nil
}

func (System) Status(ctx context.Context, receipt Receipt) (State, error) {
	if strings.TrimSpace(receipt.Printer) == "" || strings.TrimSpace(receipt.JobID) == "" {
		return StateUnknown, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	active, activeErr := run(ctx, "lpstat", "-W", "not-completed", "-o")
	if activeErr == nil && cupsListHasJob(string(active), receipt.JobID) {
		return StateQueued, nil
	}
	completed, completedErr := run(ctx, "lpstat", "-W", "completed", "-o")
	if completedErr == nil {
		switch cupsCompletedState(string(completed), receipt.JobID) {
		case StateCompleted:
			return StateCompleted, nil
		case StateFailed:
			return StateFailed, nil
		}
	}
	if activeErr == nil || completedErr == nil {
		return StateUnknown, nil
	}
	return StateUnknown, cupsError("无法读取打印任务状态", append(active, completed...), errors.Join(activeErr, completedErr))
}

func cupsCapabilities(output string, defaults domain.PrintCapabilities) domain.PrintCapabilities {
	capabilities := defaults
	for _, line := range strings.Split(output, "\n") {
		name, values, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		name = strings.TrimSpace(strings.SplitN(name, "/", 2)[0])
		fields := strings.Fields(values)
		for i := range fields {
			fields[i] = strings.TrimPrefix(fields[i], "*")
		}
		switch name {
		case "PageSize", "media":
			papers := make([]string, 0, len(fields))
			for _, value := range fields {
				switch strings.ToLower(value) {
				case "a4":
					papers = appendUnique(papers, "A4")
				case "a3":
					papers = appendUnique(papers, "A3")
				case "letter":
					papers = appendUnique(papers, "Letter")
				case "legal":
					papers = appendUnique(papers, "Legal")
				}
			}
			if len(papers) > 0 {
				capabilities.Papers = papers
			}
		case "sides":
			modes := make([]string, 0, len(fields))
			for _, value := range fields {
				switch value {
				case "one-sided":
					modes = appendUnique(modes, domain.PrintDuplexSimplex)
				case "two-sided-long-edge":
					modes = appendUnique(modes, domain.PrintDuplexLongEdge)
				case "two-sided-short-edge":
					modes = appendUnique(modes, domain.PrintDuplexShortEdge)
				}
			}
			if len(modes) > 0 {
				capabilities.DuplexModes = modes
			}
		case "ColorModel", "print-color-mode":
			colors := make([]string, 0, len(fields))
			for _, value := range fields {
				if strings.Contains(strings.ToLower(value), "gray") || strings.Contains(strings.ToLower(value), "mono") ||
					strings.Contains(strings.ToLower(value), "black") {
					colors = appendUnique(colors, domain.PrintColorMonochrome)
				} else {
					colors = appendUnique(colors, domain.PrintColorColor)
				}
			}
			if len(colors) > 0 {
				capabilities.ColorModes = colors
			}
		case "orientation-requested":
			if len(fields) > 0 {
				capabilities.Orientations = normalizedChoices(nil, defaults.Orientations)
			}
		}
	}
	return capabilities
}

func cupsCompletedState(output, jobID string) State {
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 || fields[0] != jobID {
			continue
		}
		lower := strings.ToLower(line)
		if strings.Contains(lower, "cancelled") || strings.Contains(lower, "aborted") {
			return StateFailed
		}
		if strings.Contains(lower, "completed") {
			return StateCompleted
		}
	}
	return StateUnknown
}

func cupsListHasJob(output, jobID string) bool {
	for _, line := range strings.Split(output, "\n") {
		if fields := strings.Fields(line); len(fields) > 0 && fields[0] == jobID {
			return true
		}
	}
	return false
}

func cupsError(prefix string, output []byte, err error) error {
	detail := strings.TrimSpace(string(output))
	if detail == "" {
		detail = err.Error()
	}
	return fmt.Errorf("%s：%s", prefix, detail)
}

func appendUnique(values []string, value string) []string {
	if !slices.Contains(values, value) {
		return append(values, value)
	}
	return values
}

func uniqueLines(value string) []string {
	lines := strings.Split(value, "\n")
	out := lines[:0]
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" && !slices.Contains(out, line) {
			out = append(out, line)
		}
	}
	return out
}

var run = func(ctx context.Context, executable string, args ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, executable, args...)
	return command.CombinedOutput()
}
