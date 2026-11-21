package domain

import (
	"encoding/json"
	"testing"
)

func TestWallpaperTransparencyMigration(t *testing.T) {
	for _, tc := range []struct {
		json string
		want int
	}{
		{`{"Theme":"light","BackgroundShade":0}`, 60},
		{`{"Theme":"light","BackgroundShade":100}`, 71},
		{`{"Theme":"dark","BackgroundShade":50}`, 79},
		{`{"Theme":"dark","BackgroundTransparency":0}`, 0},
		{`{"BackgroundTransparency":100}`, 100},
	} {
		var s Settings
		if err := json.Unmarshal([]byte(tc.json), &s); err != nil || s.BackgroundTransparency != tc.want {
			t.Fatalf("%s: transparency %d, error %v", tc.json, s.BackgroundTransparency, err)
		}
	}
}
