package domain

import "encoding/json"

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
	*s = Settings(wire.settingsValue)
	return nil
}
