// Package printing adapts GoIM's portable print model to the host print
// service. It never opens a print dialog: jobs are submitted directly to an
// installed queue.
package printing

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"goim/internal/domain"
)

const (
	MaxFileSize     = int64(256 << 20)
	MaxSelectedPage = 200
)

type Backend interface {
	List(context.Context) ([]Printer, error)
	Submit(context.Context, string, string, domain.PrintOptions) (Receipt, error)
	Status(context.Context, Receipt) (State, error)
}

type System struct{}

type Printer struct {
	ID           string
	Name         string
	Available    bool
	Default      bool
	Status       string
	Capabilities domain.PrintCapabilities
}

type Receipt struct {
	Printer string
	JobID   string
}

type State string

const (
	StateUnknown   State = "unknown"
	StateQueued    State = "queued"
	StateCompleted State = "completed"
	StateFailed    State = "failed"
)

func DefaultCapabilities() domain.PrintCapabilities {
	return domain.PrintCapabilities{
		Papers:            []string{"A4", "A3", "Letter", "Legal"},
		Orientations:      []string{domain.PrintOrientationPortrait, domain.PrintOrientationLandscape},
		ColorModes:        []string{domain.PrintColorColor, domain.PrintColorMonochrome},
		DuplexModes:       []string{domain.PrintDuplexSimplex, domain.PrintDuplexLongEdge, domain.PrintDuplexShortEdge},
		SupportsPageRange: true,
	}
}

var pageRangePattern = regexp.MustCompile(`^[0-9]+(?:-[0-9]+)?(?:,[0-9]+(?:-[0-9]+)?)*$`)

func NormalizeOptions(options domain.PrintOptions, capabilities domain.PrintCapabilities) (domain.PrintOptions, error) {
	options.Paper = strings.TrimSpace(options.Paper)
	options.Orientation = strings.TrimSpace(options.Orientation)
	options.PageRange = strings.TrimSpace(options.PageRange)
	options.Color = strings.TrimSpace(options.Color)
	options.Duplex = strings.TrimSpace(options.Duplex)
	if options.Copies == 0 {
		options.Copies = 1
	}
	if options.Copies < 1 || options.Copies > 999 {
		return domain.PrintOptions{}, errors.New("打印份数需在 1 到 999 之间")
	}
	if options.Paper == "" {
		options.Paper = firstChoice(capabilities.Papers, "A4")
	}
	if !choiceAllowed(options.Paper, capabilities.Papers, []string{"A4", "A3", "Letter", "Legal"}) {
		return domain.PrintOptions{}, errors.New("打印机不支持所选纸张")
	}
	if options.Orientation == "" {
		options.Orientation = domain.PrintOrientationPortrait
	}
	if !choiceAllowed(options.Orientation, capabilities.Orientations,
		[]string{domain.PrintOrientationPortrait, domain.PrintOrientationLandscape}) {
		return domain.PrintOptions{}, errors.New("打印机不支持所选方向")
	}
	if options.Color == "" {
		options.Color = domain.PrintColorMonochrome
	}
	if !choiceAllowed(options.Color, capabilities.ColorModes,
		[]string{domain.PrintColorColor, domain.PrintColorMonochrome}) {
		return domain.PrintOptions{}, errors.New("打印机不支持所选色彩模式")
	}
	if options.Duplex == "" {
		options.Duplex = domain.PrintDuplexSimplex
	}
	if !choiceAllowed(options.Duplex, capabilities.DuplexModes,
		[]string{domain.PrintDuplexSimplex, domain.PrintDuplexLongEdge, domain.PrintDuplexShortEdge}) {
		return domain.PrintOptions{}, errors.New("打印机不支持所选单双面模式")
	}
	if options.PageRange != "" {
		if !capabilities.SupportsPageRange {
			return domain.PrintOptions{}, errors.New("打印机不支持自定义页码范围")
		}
		if len(options.PageRange) > 64 || !pageRangePattern.MatchString(strings.ReplaceAll(options.PageRange, " ", "")) {
			return domain.PrintOptions{}, errors.New("页码范围格式应为 1-3,5")
		}
		options.PageRange = strings.ReplaceAll(options.PageRange, " ", "")
		for _, part := range strings.Split(options.PageRange, ",") {
			limits := strings.Split(part, "-")
			var first, last int
			if _, err := fmt.Sscanf(limits[0], "%d", &first); err != nil || first < 1 {
				return domain.PrintOptions{}, errors.New("页码范围必须从第 1 页或之后开始")
			}
			if len(limits) == 2 {
				if _, err := fmt.Sscanf(limits[1], "%d", &last); err != nil || last < first {
					return domain.PrintOptions{}, errors.New("页码范围结束值不能小于起始值")
				}
			}
		}
		if selectedPageCount(options.PageRange) > MaxSelectedPage {
			return domain.PrintOptions{}, fmt.Errorf("一次最多打印 %d 页", MaxSelectedPage)
		}
	}
	return options, nil
}

func selectedPageCount(pageRange string) int {
	count := 0
	for _, part := range strings.Split(pageRange, ",") {
		limits := strings.Split(part, "-")
		first := 0
		_, _ = fmt.Sscanf(limits[0], "%d", &first)
		if len(limits) == 2 {
			last := 0
			_, _ = fmt.Sscanf(limits[1], "%d", &last)
			count += last - first + 1
		} else {
			count++
		}
	}
	return count
}

func ValidateFile(path string) (string, error) {
	path, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("请选择文件，暂不支持直接打印文件夹")
	}
	if info.Size() <= 0 {
		return "", errors.New("空文件无法打印")
	}
	if info.Size() > MaxFileSize {
		return "", fmt.Errorf("文件超过 %d MB 的打印上限", MaxFileSize>>20)
	}
	return path, nil
}

func NormalizeCapabilities(capabilities domain.PrintCapabilities) domain.PrintCapabilities {
	defaults := DefaultCapabilities()
	out := domain.PrintCapabilities{
		Papers:            normalizedChoices(capabilities.Papers, defaults.Papers),
		Orientations:      normalizedChoices(capabilities.Orientations, defaults.Orientations),
		ColorModes:        normalizedChoices(capabilities.ColorModes, defaults.ColorModes),
		DuplexModes:       normalizedChoices(capabilities.DuplexModes, defaults.DuplexModes),
		SupportsPageRange: capabilities.SupportsPageRange,
	}
	if len(out.Papers) == 0 {
		out.Papers = slices.Clone(defaults.Papers)
	}
	if len(out.Orientations) == 0 {
		out.Orientations = slices.Clone(defaults.Orientations)
	}
	if len(out.ColorModes) == 0 {
		out.ColorModes = slices.Clone(defaults.ColorModes)
	}
	if len(out.DuplexModes) == 0 {
		out.DuplexModes = slices.Clone(defaults.DuplexModes)
	}
	return out
}

func normalizedChoices(values, fallback []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" && !slices.Contains(out, value) {
			out = append(out, value)
		}
	}
	if len(out) == 0 {
		return slices.Clone(fallback)
	}
	return out
}

func firstChoice(values []string, fallback string) string {
	if len(values) > 0 && strings.TrimSpace(values[0]) != "" {
		return values[0]
	}
	return fallback
}

func choiceAllowed(value string, supported, defaults []string) bool {
	if len(supported) == 0 {
		return slices.Contains(defaults, value)
	}
	return slices.Contains(supported, value)
}
