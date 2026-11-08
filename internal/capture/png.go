package capture

import (
	"image"
	"image/png"
	"os"
	"path/filepath"
)

func writePNGAtomic(path string, output *image.RGBA) (err error) {
	directory := filepath.Dir(path)
	file, err := os.CreateTemp(directory, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	temporary := file.Name()
	defer func() {
		if err != nil {
			_ = os.Remove(temporary)
		}
	}()
	if err = png.Encode(file, output); err != nil {
		_ = file.Close()
		return err
	}
	if err = file.Chmod(0o644); err != nil {
		_ = file.Close()
		return err
	}
	if err = file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	if err = os.Rename(temporary, path); err != nil {
		return err
	}
	return nil
}
