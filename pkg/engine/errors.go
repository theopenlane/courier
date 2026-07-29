package engine

import (
	"errors"
	"fmt"
	"strings"

	"github.com/Yamashou/gqlgenc/clientv2"
	"github.com/samber/lo"
	"github.com/vektah/gqlparser/v2/gqlerror"
)

// alreadyExistsCode is the GraphQL error code for records that already exist
const alreadyExistsCode = "ALREADY_EXISTS"

var (
	// ErrMissingToken is returned when no API token is configured
	ErrMissingToken = errors.New("missing API token, set COURIER_TOKEN, add token to config/.config.yaml, or pass --token")

	// ErrMultipleControlsFound is returned when a mapped control refCode matches more than one control
	ErrMultipleControlsFound = errors.New("multiple controls found for refCode")

	// ErrMissingMarkdown is returned when a policy manifest entry references a markdown file that does not exist
	ErrMissingMarkdown = errors.New("policy markdown file not found")

	// ErrUnrecognizedFile is returned when a file does not validate as a control inventory or a policy manifest
	ErrUnrecognizedFile = errors.New("file is not a recognizable control inventory or policy manifest")
)

// fmtErr wraps a sentinel error with a detail value
func fmtErr(err error, detail string) error {
	return fmt.Errorf("%w: %q", err, detail)
}

// isAlreadyExists reports whether an API error indicates the record already
// exists, creates tolerate this so re-running apply stays idempotent
func isAlreadyExists(err error) bool {
	if err == nil {
		return false
	}

	var gqlErr *clientv2.ErrorResponse
	if errors.As(err, &gqlErr) && gqlErr.GqlErrors != nil {
		exists := lo.SomeBy(*gqlErr.GqlErrors, func(e *gqlerror.Error) bool {
			code, ok := e.Extensions["code"].(string)

			return ok && code == alreadyExistsCode
		})
		if exists {
			return true
		}
	}

	return strings.Contains(err.Error(), "already exists")
}
