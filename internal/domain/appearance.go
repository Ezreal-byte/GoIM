package domain

import (
	"encoding/json"
	"slices"
)

func ColorSchemes() [4]string { return [4]string{"classic", "pine", "clay", "iris"} }

func ValidColorScheme(value string) bool {
	schemes := ColorSchemes()
	return slices.Contains(schemes[:], value)
}

// UnmarshalJSON carries the old wallpaper overlay strength forward as a
// transparency percentage. An explicitly saved 0 means a fully visible image.
func (s *Settings) UnmarshalJSON(data []byte) error {
	type settingsValue Settings
	var wire struct {
		settingsValue
		BackgroundShade int
	}
	wire.BackgroundTransparency = -1
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	if wire.BackgroundTransparency == -1 {
		shade := min(max(wire.BackgroundShade, 0), 100)
		wire.BackgroundTransparency = 60 + (11*shade+50)/100
		if wire.Theme == "dark" {
			wire.BackgroundTransparency = 63 + (32*shade+50)/100
		}
	}
	if !ValidColorScheme(wire.ColorScheme) {
		wire.ColorScheme = "classic"
	}
	*s = Settings(wire.settingsValue)
	return nil
}
