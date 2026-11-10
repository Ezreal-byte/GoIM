//go:build !windows || vivahead

package desktop

// Viva reports hidden/minimized state on these backends; the headless driver
// also deliberately operates without a real foreground window.
func mainWindowForeground() bool { return true }
