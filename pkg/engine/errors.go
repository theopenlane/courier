package engine

import (
	"errors"
	"fmt"
	"strings"

	"github.com/Yamashou/gqlgenc/clientv2"
)

const (
	// alreadyExistsCode is the GraphQL error code for records that already exist
	alreadyExistsCode = "ALREADY_EXISTS"
	// alreadyExistsMessage is the wording used when the conflict carries no error code
	alreadyExistsMessage = "already exists"
)

var (
	// ErrMissingToken is returned when no API token is configured
	ErrMissingToken = errors.New("missing API token, set COURIER_TOKEN, add token to config/.config.yaml, or pass --token")

	// ErrMissingHost is returned when no API host is configured
	ErrMissingHost = errors.New("missing API host, set COURIER_HOST, add host to config/.config.yaml, or pass --host")

	// ErrMultipleControlsFound is returned when a mapped control refCode matches more than one control
	ErrMultipleControlsFound = errors.New("multiple controls found for refCode")

	// ErrMissingMarkdown is returned when a policy manifest entry references a markdown file that does not exist
	ErrMissingMarkdown = errors.New("policy markdown file not found")

	// ErrUnrecognizedFile is returned when a file does not validate as a control inventory or a policy manifest
	ErrUnrecognizedFile = errors.New("file is not a recognizable control inventory or policy manifest")

	// ErrPaginationStalled is returned when the API reports another page but does not advance the cursor
	ErrPaginationStalled = errors.New("pagination stalled, the API reported another page without advancing the cursor")
)

// fmtErr wraps a sentinel error with a detail value
func fmtErr(err error, detail string) error {
	return fmt.Errorf("%w: %q", err, detail)
}

// isAlreadyExists reports whether an API error indicates the record already exists.
// Other constraint violations use the same wording under a CONFLICT code, so the
// message is only consulted when the error carries no code
func isAlreadyExists(err error) bool {
	if err == nil {
		return false
	}

	var gqlErr *clientv2.ErrorResponse
	if errors.As(err, &gqlErr) && gqlErr.GqlErrors != nil {
		var coded bool

		for _, e := range *gqlErr.GqlErrors {
			code, ok := e.Extensions["code"].(string)
			if !ok {
				continue
			}

			if code == alreadyExistsCode {
				return true
			}

			coded = true
		}

		if coded {
			return false
		}
	}

	return strings.Contains(err.Error(), alreadyExistsMessage)
}
