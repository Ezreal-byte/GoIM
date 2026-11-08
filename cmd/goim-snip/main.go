package main

import (
	"errors"
	"flag"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"io"
	"os"
	"path/filepath"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/ui"

	"goim/internal/capture"
)

const maxInputPixels = 100_000_000

type options struct {
	output  string
	input   string
	display int
	all     bool
}

func main() {
	os.Exit(run(os.Args[1:], os.Stderr))
}

func run(args []string, stderr io.Writer) int {
	options, err := parseOptions(args, stderr)
	if errors.Is(err, flag.ErrHelp) {
		return 0
	}
	if err != nil {
		fmt.Fprintf(stderr, "goim-snip: %v\n", err)
		return 1
	}

	source, err := loadSource(options)
	if err != nil {
		fmt.Fprintf(stderr, "goim-snip: %v\n", err)
		return 1
	}
	document, err := capture.NewDocument(source.image)
	if err != nil {
		fmt.Fprintf(stderr, "goim-snip: open screenshot: %v\n", err)
		return 1
	}
	width, height := initialWindowSize(source.displays)
	editor, err := newEditor(document, options.output, source.info, initialDark(), width, height)
	if err != nil {
		fmt.Fprintf(stderr, "goim-snip: prepare editor: %v\n", err)
		return 1
	}
	editor.followSystem = true

	var invalidator app.Invalidator
	stopAppearance := app.WatchSystemAppearance(invalidator.Invalidate)
	defer stopAppearance()

	var canvas ui.Canvas
	runErr := app.RunApplication(func(application *app.Application) error {
		var window *app.Window
		editor.closeWindow = func() {
			if window != nil {
				window.Close()
				return
			}
			application.Quit()
		}
		opened, err := application.OpenWindow(editor.config(&invalidator), &canvas)
		if err != nil {
			return err
		}
		window = opened
		return nil
	})
	if runErr != nil {
		fmt.Fprintf(stderr, "goim-snip: editor: %v\n", runErr)
		return 1
	}
	if editor.err != nil {
		fmt.Fprintf(stderr, "goim-snip: %v\n", editor.err)
		return 1
	}
	switch editor.outcome {
	case outcomeConfirmed:
		return 0
	case outcomeCanceled:
		return 2
	default:
		return 1
	}
}

func parseOptions(args []string, output io.Writer) (options, error) {
	var parsed options
	flags := flag.NewFlagSet("goim-snip", flag.ContinueOnError)
	flags.SetOutput(output)
	flags.StringVar(&parsed.output, "output", "", "write the confirmed PNG to this path (required)")
	flags.StringVar(&parsed.input, "input", "", "edit an existing PNG instead of capturing the screen")
	flags.IntVar(&parsed.display, "display", -1, "capture this display index (default: the whole desktop)")
	flags.BoolVar(&parsed.all, "all", false, "capture the whole virtual desktop (the default)")
	flags.Usage = func() {
		fmt.Fprintln(output, "usage: goim-snip -output PATH [-display INDEX | -all] [-input PNG]")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		return options{}, err
	}
	if flags.NArg() != 0 {
		return options{}, fmt.Errorf("unexpected argument %q", flags.Arg(0))
	}
	if parsed.output == "" {
		flags.Usage()
		return options{}, errors.New("-output is required")
	}
	if parsed.display < -1 {
		return options{}, fmt.Errorf("-display must be zero or greater")
	}
	if parsed.all && parsed.display >= 0 {
		return options{}, errors.New("-all cannot be combined with -display")
	}
	if parsed.input != "" && (parsed.all || parsed.display >= 0) {
		return options{}, errors.New("-input cannot be combined with -display or -all")
	}
	return parsed, nil
}

type sourceData struct {
	image    image.Image
	bounds   image.Rectangle
	displays []capture.Display
	info     sourceInfo
}

type sourceInfo struct {
	title    string
	detail   string
	displays int
	dpi      uint32
	imported bool
}

func loadSource(options options) (sourceData, error) {
	if options.input != "" {
		picture, bounds, err := decodeInput(options.input)
		if err != nil {
			return sourceData{}, err
		}
		return sourceData{
			image:  picture,
			bounds: bounds,
			info: sourceInfo{
				title:    "Imported PNG",
				detail:   filepath.Base(options.input),
				imported: true,
			},
		}, nil
	}

	var desktop *capture.Desktop
	var err error
	if options.display >= 0 {
		desktop, err = capture.CaptureDisplay(options.display)
	} else {
		desktop, err = capture.CaptureAll()
	}
	if err != nil {
		return sourceData{}, fmt.Errorf("capture: %w", err)
	}
	title := "All displays"
	if options.display >= 0 {
		title = fmt.Sprintf("Display %d", options.display)
	}
	var dpi uint32
	if len(desktop.Displays) > 0 {
		dpi = desktop.Displays[0].DPI
	}
	return sourceData{
		image:    desktop.Image,
		bounds:   desktop.Bounds,
		displays: desktop.Displays,
		info: sourceInfo{
			title:    title,
			detail:   fmt.Sprintf("%s · %d × %d px", displayCount(len(desktop.Displays)), desktop.Bounds.Dx(), desktop.Bounds.Dy()),
			displays: len(desktop.Displays),
			dpi:      dpi,
		},
	}, nil
}

func decodeInput(path string) (*image.RGBA, image.Rectangle, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, image.Rectangle{}, fmt.Errorf("open input PNG: %w", err)
	}
	defer file.Close()
	config, err := png.DecodeConfig(file)
	if err != nil {
		return nil, image.Rectangle{}, fmt.Errorf("read input PNG: %w", err)
	}
	if config.Width <= 0 || config.Height <= 0 {
		return nil, image.Rectangle{}, errors.New("input PNG has no pixels")
	}
	if int64(config.Width)*int64(config.Height) > maxInputPixels {
		return nil, image.Rectangle{}, fmt.Errorf("input PNG is larger than %d pixels", maxInputPixels)
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return nil, image.Rectangle{}, fmt.Errorf("rewind input PNG: %w", err)
	}
	decoded, err := png.Decode(file)
	if err != nil {
		return nil, image.Rectangle{}, fmt.Errorf("decode input PNG: %w", err)
	}
	bounds := decoded.Bounds()
	owned := image.NewRGBA(image.Rect(0, 0, bounds.Dx(), bounds.Dy()))
	draw.Draw(owned, owned.Bounds(), decoded, bounds.Min, draw.Src)
	return owned, image.Rect(0, 0, bounds.Dx(), bounds.Dy()), nil
}

func displayCount(count int) string {
	if count == 1 {
		return "1 display"
	}
	return fmt.Sprintf("%d displays", count)
}

func initialWindowSize(displays []capture.Display) (int, int) {
	const (
		defaultWidth  = 1180
		defaultHeight = 780
		minWidth      = 820
		minHeight     = 600
		maxWidth      = 1240
		maxHeight     = 860
		windowMargin  = 64
	)
	if len(displays) == 0 {
		return defaultWidth, defaultHeight
	}
	display := displays[0]
	for _, candidate := range displays {
		if candidate.Primary {
			display = candidate
			break
		}
	}
	if display.Work.Empty() {
		return defaultWidth, defaultHeight
	}
	scale := display.Scale()
	width := int(float64(display.Work.Dx())/scale) - windowMargin
	height := int(float64(display.Work.Dy())/scale) - windowMargin
	width = min(max(width, minWidth), maxWidth)
	height = min(max(height, minHeight), maxHeight)
	return width, height
}

func initialDark() bool {
	return app.SystemAppearance() == app.AppearanceDark
}
