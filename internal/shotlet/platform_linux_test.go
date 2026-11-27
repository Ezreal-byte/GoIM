package shotlet

import "testing"

func TestLinuxCaptureRequiresAnX11Desktop(t *testing.T) {
	for _, test := range []struct {
		session, display, wayland string
		want                      bool
	}{
		{"x11", ":1", "", true}, {"", ":2", "", true},
		{"wayland", ":1", "wayland-0", false}, {"wayland", ":1", "", false},
		{"", ":1", "wayland-0", false}, {"x11", "", "", false},
	} {
		t.Run(test.session+test.display+test.wayland, func(t *testing.T) {
			t.Setenv("XDG_SESSION_TYPE", test.session)
			t.Setenv("DISPLAY", test.display)
			t.Setenv("WAYLAND_DISPLAY", test.wayland)
			if Supported() != test.want {
				t.Fatal("incorrect desktop capability")
			}
		})
	}
}
