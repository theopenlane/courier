package cmd

import "errors"

// ErrNotFormatted is returned by fmt --check when files are not canonical
var ErrNotFormatted = errors.New("files are not in canonical form, run 'courier fmt'")

// ErrApplyIncomplete is returned when some records failed to apply
var ErrApplyIncomplete = errors.New("apply completed with errors")
