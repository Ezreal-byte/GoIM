// A subprocess fixture for the host protocol, never shipped with GoIM.
package main

import (
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"os"
	"strconv"
)

func main() {
	if len(os.Args) < 2 {
		os.Exit(1)
	}
	if os.Args[1] == "--permissions" {
		_, _ = os.Stdout.WriteString(`{"version":1,"settings_available":false,"screen_recording":true}`)
		return
	}
	if os.Args[1] == "--self-test" {
		if os.Getenv("GOIM_SHOTLET_TEST_BROKEN") != "" {
			os.Exit(127)
		}
	} else if os.Args[1] == "-select" {
		_ = os.WriteFile(os.Getenv("GOIM_SHOTLET_TEST_FALLBACK"), []byte("used"), 0600)
		for i, argument := range os.Args {
			if argument == "-output" && i+1 < len(os.Args) {
				file, err := os.Create(os.Args[i+1])
				if err != nil {
					panic(err)
				}
				picture := image.NewNRGBA(image.Rect(0, 0, 1, 1))
				picture.SetNRGBA(0, 0, color.NRGBA{G: 255, A: 255})
				if err := png.Encode(file, picture); err != nil {
					panic(err)
				}
				if err := file.Close(); err != nil {
					panic(err)
				}
				return
			}
		}
		os.Exit(1)
	} else if code, _ := strconv.Atoi(os.Getenv("GOIM_SHOTLET_TEST_RESULT")); code != 0 {
		os.Exit(code)
	}
	frame := binary.LittleEndian.AppendUint32([]byte("ZMSHOT1\n"), 1)
	frame = binary.LittleEndian.AppendUint32(frame, 1)
	_, _ = os.Stdout.Write(append(frame, 255, 0, 0, 255))
}
