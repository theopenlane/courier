package controlfile

import "errors"

var (
	// ErrSchemaValidation is returned when a document does not conform to its JSON schema
	ErrSchemaValidation = errors.New("document does not conform to schema")

	// ErrMissingFrontmatter is returned when a policy markdown document has no frontmatter block
	ErrMissingFrontmatter = errors.New("policy markdown is missing a frontmatter block")
)
