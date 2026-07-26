package controlfile

import (
	"fmt"
	"strings"

	"github.com/theopenlane/core/pkg/jsonx"
)

// validateSchema validates a document against the JSON schema reflected from
// its Go type, record-level constraints such as refCode uniqueness are
// enforced by the API
func validateSchema[T any](document any) error {
	result, err := jsonx.ValidateSchema(jsonx.SchemaFrom[T](), document)
	if err != nil {
		return err
	}

	if result.Valid() {
		return nil
	}

	return fmt.Errorf("%w: %s", ErrSchemaValidation, strings.Join(jsonx.ValidationErrorStrings(result), "; "))
}

// ValidateControls validates the control inventory against its JSON schema,
// an empty inventory is valid
func ValidateControls(controls []*Control) error {
	if len(controls) == 0 {
		return nil
	}

	return validateSchema[[]*Control](controls)
}

// ValidatePolicies validates the policy manifest against its JSON schema, an
// empty manifest is valid
func ValidatePolicies(policies []*Policy) error {
	if len(policies) == 0 {
		return nil
	}

	return validateSchema[[]*Policy](policies)
}
