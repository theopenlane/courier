package cmd

import (
	"errors"
	"fmt"
)

// ErrNotFormatted is returned by fmt --check when files are not canonical
var ErrNotFormatted = errors.New("files are not in canonical form, run 'courier fmt'")

// ErrApplyIncomplete is returned when some records failed to apply
var ErrApplyIncomplete = errors.New("apply completed with errors")

// ErrUnknownConvertKind is returned when convert cannot resolve the document
// kind an input converts into
var ErrUnknownConvertKind = errors.New("cannot tell what kind of document the input holds")

// ErrConvertOutputExists is returned when convert would overwrite an existing file
var ErrConvertOutputExists = errors.New("output file already exists")

// ErrNotAControlInventory is returned when a file named to archivable holds
// something other than controls
var ErrNotAControlInventory = errors.New("file is not a control inventory")

// fmtCmdErr wraps a sentinel error with a detail value
func fmtCmdErr(err error, detail string) error {
	return fmt.Errorf("%w: %s", err, detail)
}
