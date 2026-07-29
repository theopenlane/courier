package controlfile

import (
	"path"
	"regexp"
	"strings"
)

// MarkdownExtension is the file extension for policy markdown documents
const MarkdownExtension = ".md"

var (
	// unsafePathChars matches characters that cannot be used in file names
	unsafePathChars = regexp.MustCompile(`[^A-Za-z0-9._-]+`)
	// repeatedDashes matches dash runs left behind by character replacement
	repeatedDashes = regexp.MustCompile(`-{2,}`)
)

// PolicyMarkdownPath returns the canonical store-relative markdown path
// for a policy name, e.g. policies/application-security-policy.md
func PolicyMarkdownPath(name string) string {
	return path.Join(PoliciesDir, slug(strings.ToLower(name))+MarkdownExtension)
}

// slug replaces path-unsafe characters so names can be used as file names
func slug(s string) string {
	s = unsafePathChars.ReplaceAllString(strings.TrimSpace(s), "-")
	s = repeatedDashes.ReplaceAllString(s, "-")

	return strings.Trim(s, "-.")
}
