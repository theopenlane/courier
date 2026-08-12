package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/theopenlane/courier/pkg/convert"
)

const (
	// dirPerm is the permission mode for created directories
	dirPerm = 0o755
	// filePerm is the permission mode for a written store file
	filePerm = 0o644
)

// resolveOutput resolves the file a command writes, an empty path writes to
// stdout, write targets the kind's file in the store directory
func resolveOutput(kind convert.Kind, output string, write bool) (string, error) {
	if output != "" {
		return output, nil
	}

	if !write {
		return "", nil
	}

	settings, err := loadSettings()
	if err != nil {
		return "", err
	}

	file, err := convert.StoreFile(kind)
	if err != nil {
		return "", err
	}

	return filepath.Join(settings.Dir, file), nil
}

// writeOutput writes rendered yaml, an existing file is only replaced with
// force so a converted or merged input never silently overwrites the store
func writeOutput(path string, data []byte, force bool) error {
	// the path comes from a flag, the caller writes where they asked to
	path = filepath.Clean(path)

	if !force {
		if _, err := os.Stat(path); err == nil {
			return fmt.Errorf("%w: %s, pass --force to replace it", ErrConvertOutputExists, path)
		}
	}

	if err := os.MkdirAll(filepath.Dir(path), dirPerm); err != nil {
		return err
	}

	return os.WriteFile(path, data, filePerm) //nolint:gosec // the path is the file the caller asked to write
}
