package cmd

import "errors"

// ErrNotFormatted is returned by fmt --check when files are not canonical
var ErrNotFormatted = errors.New("files are not in canonical form, run 'courier fmt'")
