package engine

import (
	"html"
	"regexp"
	"strings"

	"github.com/JohannesKaufmann/html-to-markdown/v2/converter"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/base"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/commonmark"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/table"
	"github.com/microcosm-cc/bluemonday"
	"github.com/theopenlane/newman/scrubber"
)

// scrub reduces rendered rich-text HTML to clean semantic markup, the same
// scrubber configuration the export job uses to render policy documents
var scrub = scrubber.NewPolicyScrubber(
	scrubber.WithStyling(),
	scrubber.WithTables(),
	scrubber.WithImages(),
	scrubber.WithDocumentStructure(),
	scrubber.WithAccessibility(),
	scrubber.WithURLSchemes("http", "https", "mailto", "tel"),
	scrubber.WithNoRelativeURLs(),
	scrubber.WithTargetBlankOnLinks(),
)

// adjacentListRegex matches the boundary between consecutive single-item
// lists produced by the rich-text editor, which renders every numbered item
// as its own list element
var adjacentListRegex = regexp.MustCompile(`</(ol|ul)>\s*(?:</div>\s*<div[^>]*>)?\s*<(?:ol|ul)[^>]*>`)

// listSeparatorRegex matches the comment separators the converter inserts
// between adjacent lists
var listSeparatorRegex = regexp.MustCompile(`\n*<!--THE END-->\n*`)

// markdownConverter converts semantic HTML into markdown including tables
var markdownConverter = converter.NewConverter(
	converter.WithPlugins(
		base.NewBasePlugin(),
		commonmark.NewCommonmarkPlugin(),
		table.NewTablePlugin(),
	),
)

// bodyToMarkdown renders a stored document body as markdown, rendered
// rich-text HTML is scrubbed to semantic markup and converted, content
// already authored as markdown passes through unchanged so the conversion is
// deterministic and applied markdown round-trips verbatim
func bodyToMarkdown(body string) string {
	if !htmlTagRegex.MatchString(body) {
		return body
	}

	cleaned := scrub.Scrub(body)
	cleaned = strings.ReplaceAll(cleaned, "\ufeff", "")
	// merge the editor's one-item-per-list rendering into single lists so
	// numbering increments naturally
	cleaned = adjacentListRegex.ReplaceAllString(cleaned, "")

	markdown, err := markdownConverter.ConvertString(cleaned)
	if err != nil {
		// fall back to the scrubbed markup rather than losing content
		return strings.TrimSpace(cleaned)
	}

	markdown = listSeparatorRegex.ReplaceAllString(markdown, "\n\n")

	return strings.TrimSpace(markdown)
}

// stripper removes all HTML markup, used to render rich-text fields as plain
// text in the inventory
var stripper = bluemonday.StrictPolicy()

// htmlTagRegex detects the presence of an HTML element tag, used to
// distinguish rendered rich-text content from plain text
var htmlTagRegex = regexp.MustCompile(`</?[a-zA-Z][a-zA-Z0-9-]*(\s[^<>]*)?/?>`)

// plainText renders a rich-text field as plain text, values without HTML
// markup pass through unchanged so plainText is idempotent and file values
// compare cleanly against stripped remote values
func plainText(value string) string {
	if !htmlTagRegex.MatchString(value) {
		return value
	}

	stripped := html.UnescapeString(stripper.Sanitize(value))
	stripped = strings.ReplaceAll(stripped, "\ufeff", "")

	return strings.Join(strings.Fields(stripped), " ")
}
