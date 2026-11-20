package domain

import "testing"

func TestAutomaticVideoBoundary(t *testing.T) {
	for _, c := range []struct {
		name string
		size int64
		want bool
	}{{"clip.MP4", AutoVideoLimit - 1, true}, {"clip.mp4", AutoVideoLimit, false}, {"clip.mp4", AutoVideoLimit + 1, false}, {"clip.mp4", 0, false}, {"movie.mp4.exe", 10, false}, {"script.ts", 10, false}} {
		f := File{Name: c.name, Size: c.size, MIME: "video/mp4"}
		if f.AutoReceiveVideo(Settings{}) != c.want {
			t.Error(c)
		}
		if f.AutoReceiveVideo(Settings{ManualVideoReceive: true}) {
			t.Fatal("disabled automatic download")
		}
		f.Shared = true
		if f.AutoReceiveVideo(Settings{}) {
			t.Fatal("catalog entry downloaded")
		}
		f.Shared = false
		f.Folder = true
		if f.AutoReceiveVideo(Settings{}) {
			t.Fatal("folder downloaded")
		}
	}
}
