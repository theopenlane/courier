package controlfile

import (
	"fmt"
	"strings"

	"github.com/theopenlane/core/pkg/jsonx"
)

// Validate validates a document list against the JSON schema reflected from
// its Go type, an empty list is valid, record-level constraints such as
// refCode uniqueness are enforced by the API
func Validate[T any](documents []*T) error {
	if len(documents) == 0 {
		return nil
	}

	result, err := jsonx.ValidateSchema(jsonx.SchemaFrom[[]*T](), documents)
	if err != nil {
		return err
	}

	if result.Valid() {
		return nil
	}

	return fmt.Errorf("%w: %s", ErrSchemaValidation, strings.Join(jsonx.ValidationErrorStrings(result), "; "))
}
