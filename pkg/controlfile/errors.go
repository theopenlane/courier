package controlfile

import "errors"

var (
	// ErrSchemaValidation is returned when a document does not conform to its JSON schema
	ErrSchemaValidation = errors.New("document does not conform to schema")

	// ErrMissingFrontmatter is returned when a policy markdown document has no frontmatter block
	ErrMissingFrontmatter = errors.New("policy markdown is missing a frontmatter block")

	// ErrUnsafeMarkdownPath is returned when a manifest markdownPath resolves outside the
	// store directory or does not name a markdown document
	ErrUnsafeMarkdownPath = errors.New("unsafe markdown path")
)
