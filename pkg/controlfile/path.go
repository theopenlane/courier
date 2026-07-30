package controlfile

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

// MarkdownExtension is the file extension for policy markdown documents
const MarkdownExtension = ".md"

// digestLength is the number of hex characters in a derived file name
const digestLength = 12

var (
	// unsafePathChars matches characters that cannot be used in file names
	unsafePathChars = regexp.MustCompile(`[^A-Za-z0-9._-]+`)
	// repeatedDashes matches dash runs left behind by character replacement
	repeatedDashes = regexp.MustCompile(`-{2,}`)
)

// PolicyMarkdownPath returns the canonical store-relative markdown path
// for a policy name, e.g. policies/application-security-policy.md
func PolicyMarkdownPath(name string) string {
	return path.Join(PoliciesDir, policyFileName(name)+MarkdownExtension)
}

// PolicyMarkdownPathWithID returns the markdown path for a policy name
// disambiguated by its ULID, for names that reduce to the same file name
func PolicyMarkdownPathWithID(name, id string) string {
	return path.Join(PoliciesDir, policyFileName(name)+"-"+slug(strings.ToLower(id))+MarkdownExtension)
}

// ValidateMarkdownPath requires a manifest markdownPath to stay inside the
// store directory and name a markdown document
func ValidateMarkdownPath(rel string) error {
	if !filepath.IsLocal(filepath.FromSlash(rel)) {
		return fmt.Errorf("%w: %q resolves outside the store directory", ErrUnsafeMarkdownPath, rel)
	}

	if !strings.EqualFold(path.Ext(rel), MarkdownExtension) {
		return fmt.Errorf("%w: %q does not name a %s document", ErrUnsafeMarkdownPath, rel, MarkdownExtension)
	}

	return nil
}

// policyFileName reduces a policy name to a file name, falling back to a
// digest when the name holds no path-safe characters
func policyFileName(name string) string {
	if s := slug(strings.ToLower(name)); s != "" {
		return s
	}

	sum := sha256.Sum256([]byte(name))

	return hex.EncodeToString(sum[:])[:digestLength]
}

// slug replaces path-unsafe characters so names can be used as file names
func slug(s string) string {
	s = unsafePathChars.ReplaceAllString(strings.TrimSpace(s), "-")
	s = repeatedDashes.ReplaceAllString(s, "-")

	return strings.Trim(s, "-.")
}
