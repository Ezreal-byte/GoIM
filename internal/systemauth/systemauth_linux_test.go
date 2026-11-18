package systemauth

import "testing"

func TestProcessStartTime(t *testing.T) {
	start, err := processStartTime()
	if err != nil {
		t.Fatal(err)
	}
	if start == 0 {
		t.Fatal("process start time is zero")
	}
}
