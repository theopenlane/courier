package controlfile

import (
	"fmt"
	"strings"

	"github.com/goccy/go-yaml"
)

// frontmatterDelimiter separates the YAML frontmatter block from the body
const frontmatterDelimiter = "---"

// MarshalPolicyMarkdown renders a policy markdown document: a YAML
// frontmatter block followed by the body
func MarshalPolicyMarkdown(fm Frontmatter, body string) ([]byte, error) {
	header, err := yaml.MarshalWithOptions(fm, yamlOptions...)
	if err != nil {
		return nil, err
	}

	body = strings.TrimRight(body, "\n")

	doc := frontmatterDelimiter + "\n" + string(header) + frontmatterDelimiter + "\n"
	if body != "" {
		doc += "\n" + body + "\n"
	}

	return []byte(doc), nil
}

// SplitPolicyMarkdown parses a policy markdown document into its frontmatter
// and body, documents without a frontmatter block return the whole content as
// the body
func SplitPolicyMarkdown(data []byte) (Frontmatter, string, error) {
	content := string(data)

	rest, found := strings.CutPrefix(content, frontmatterDelimiter+"\n")
	if !found {
		return Frontmatter{}, strings.TrimSpace(content), nil
	}

	header, body, found := strings.Cut(rest, "\n"+frontmatterDelimiter+"\n")
	if !found {
		return Frontmatter{}, "", fmt.Errorf("%w: unterminated frontmatter", ErrMissingFrontmatter)
	}

	var fm Frontmatter
	if err := yaml.Unmarshal([]byte(header), &fm); err != nil {
		return Frontmatter{}, "", err
	}

	return fm, strings.TrimSpace(body), nil
}
