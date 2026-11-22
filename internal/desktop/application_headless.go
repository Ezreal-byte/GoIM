//go:build vivahead

package desktop

import "os"

func headlessApplication() bool { return os.Getenv("VIVA_HEADLESS") == "1" }
