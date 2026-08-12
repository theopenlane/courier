package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/samber/lo"

	"github.com/theopenlane/courier/pkg/engine"
)

// The report is the run written up to share, rather than the one-line-per-record
// output an operator reads: what was created, and for everything updated the
// value before and after. It renders twice from the same sections, for a
// terminal and as markdown, so what you read is what you send

// writeReport writes a rendered report to path, replacing what is there so a
// re-run refreshes the file it already wrote
func writeReport(path, report string) error {
	path = filepath.Clean(path)

	if err := os.MkdirAll(filepath.Dir(path), dirPerm); err != nil {
		return err
	}

	//nolint:gosec // the path is the file the caller asked to write
	return os.WriteFile(path, []byte(report), filePerm)
}

const (
	// reportBodyField is the one field rendered short, a policy body is a whole
	// document and the summary is there to show that it changed. Every other
	// field, a description included, renders in full: a summary that cuts off
	// what a control now says is not one anybody can review
	reportBodyField = "body"

	// reportBodyLimit caps that body
	reportBodyLimit = 240

	// reportTimeFormat stamps the run in the header
	reportTimeFormat = "2006-01-02 15:04 MST"

	// reportWidth is how wide the terminal tables run, wide enough to read a
	// description in and narrow enough for a standard terminal
	reportWidth = 100

	// reportCellPadding is the space either side of a cell, inside its rules
	reportCellPadding = 2

	// reportMinColumn is the width a column is never squeezed below, a table
	// too wide for the report gives way in its widest column until it fits,
	// and this is where that stops
	reportMinColumn = 12
)

// reportEntry is one record in a section, fields carry the values on either
// side of an update, cells what the section's own columns hold, and notes the
// mapping targets an update carries
type reportEntry struct {
	ref    string
	fields []engine.FieldDiff
	cells  []string
	notes  []string
}

// reportSection is one titled group of records the run touched, noun names
// what one row of it is and columns are what the section reports about each,
// beyond the record itself
type reportSection struct {
	title   string
	noun    string
	columns []string
	entries []reportEntry
}

// reportTotal is one count in the header table
type reportTotal struct {
	label string
	count int
}

// reportSummary is a run, in the shape both summary formats render
type reportSummary struct {
	at       time.Time
	mode     string
	totals   []reportTotal
	sections []reportSection
}

// runReport is everything a report renders from, the run itself and the
// controls Openlane still holds that the files no longer do
type runReport struct {
	result     *engine.ApplyResult
	archivable *engine.ArchivableResult
	dryRun     bool
	at         time.Time
}

// summarize arranges a run into the sections the report formats render, in the
// order a reader wants them: what was created, then what changed and how
func summarize(run runReport) reportSummary {
	result := run.result

	mode := "changes applied to Openlane"
	if run.dryRun {
		mode = "dry run, nothing was written"
	}

	summary := reportSummary{
		at:   run.at,
		mode: mode,
		totals: []reportTotal{
			{"controls created", len(result.CreatedControls)},
			{"controls updated", len(result.UpdatedControls)},
			{"controls unchanged", result.UnchangedControls},
			{"policies created", len(result.CreatedPolicies)},
			{"policies updated", len(result.UpdatedPolicies)},
			{"policies unchanged", result.UnchangedPolicies},
			{"mappings created", len(result.CreatedMappings)},
			{"mappings extended", len(result.UpdatedMappings)},
		},
	}

	// counted with the controls, an archive is a decision to make about the
	// inventory rather than something the run did
	if run.archivable != nil {
		summary.totals = append(summary.totals,
			reportTotal{"controls to remove", len(run.archivable.Controls)})
	}

	summary.add(reportSection{title: "Controls created", noun: "Control",
		entries: createdEntries(result.CreatedControls)})
	summary.add(reportSection{title: "Controls updated", noun: "Control",
		entries: updatedEntries(result.UpdatedControls)})
	summary.add(reportSection{title: "Policies created", noun: "Policy",
		entries: createdEntries(result.CreatedPolicies)})
	summary.add(reportSection{title: "Policies updated", noun: "Policy",
		entries: updatedEntries(result.UpdatedPolicies)})
	summary.add(reportSection{title: "Mappings created", noun: "Mapping", columns: []string{"References"},
		entries: targetEntries(result.CreatedMappings)})
	summary.add(reportSection{title: "Mappings extended", noun: "Mapping", columns: []string{"References"},
		entries: targetEntries(result.UpdatedMappings)})
	summary.add(reportSection{title: "Controls to remove", noun: "Control", columns: []string{"Title", "Status"},
		entries: archivableEntries(run.archivable)})
	summary.add(reportSection{title: "Warnings", noun: "Warning", entries: noteEntries(result.Warnings)})
	summary.add(reportSection{title: "Errors", noun: "Error", entries: noteEntries(result.Errors)})

	return summary
}

// add appends a section, a section with nothing in it is left out so the
// summary is only what the run did
func (s *reportSummary) add(section reportSection) {
	if len(section.entries) == 0 {
		return
	}

	s.sections = append(s.sections, section)
}

// createdEntries are the records created, a created record has no before side
func createdEntries(changes []engine.Change) []reportEntry {
	return lo.Map(changes, func(c engine.Change, _ int) reportEntry {
		return reportEntry{ref: c.Ref}
	})
}

// updatedEntries are the records updated, each with the fields that changed
// and the mapping targets a policy update also carries
func updatedEntries(changes []engine.Change) []reportEntry {
	return lo.Map(changes, func(c engine.Change, _ int) reportEntry {
		return reportEntry{ref: c.Ref, fields: c.Fields, notes: linkedTargets(c)}
	})
}

// targetEntries are the mapping records and the references added to each
func targetEntries(changes []engine.Change) []reportEntry {
	return lo.Map(changes, func(c engine.Change, _ int) reportEntry {
		return reportEntry{ref: c.Ref, cells: []string{strings.Join(c.Detail, ", ")}}
	})
}

// archivableEntries are the controls Openlane holds that the files no longer
// do, the records to review for archiving. Courier never deletes, so these are
// reported alongside the run rather than acted on
func archivableEntries(archivable *engine.ArchivableResult) []reportEntry {
	if archivable == nil {
		return nil
	}

	return lo.Map(archivable.Controls, func(c engine.ArchivableControl, _ int) reportEntry {
		ref := c.RefCode
		if c.Subcontrol {
			ref = c.ParentRefCode + "/" + c.RefCode
		}

		return reportEntry{ref: ref, cells: []string{c.Title, c.Status}}
	})
}

// noteEntries are the warnings or errors a run collected
func noteEntries(notes []string) []reportEntry {
	return lo.Map(notes, func(note string, _ int) reportEntry {
		return reportEntry{ref: note}
	})
}

// linkedTargets are the mapping targets a policy update carries, Detail holds
// the changed field names alongside them and the fields cover those already
func linkedTargets(change engine.Change) []string {
	fields := lo.SliceToMap(change.Fields, func(d engine.FieldDiff) (string, struct{}) {
		return d.Field, struct{}{}
	})

	return lo.Filter(change.Detail, func(detail string, _ int) bool {
		_, isField := fields[detail]

		return !isField
	})
}

// renderReport renders the run as markdown
func renderReport(run runReport) string {
	summary := summarize(run)

	var b strings.Builder

	b.WriteString("# Openlane apply summary\n\n")
	fmt.Fprintf(&b, "%s · %s\n\n", summary.at.Format(reportTimeFormat), summary.mode)

	b.WriteString("## Totals\n\n| Records | Count |\n| --- | ---: |\n")

	for _, total := range summary.totals {
		fmt.Fprintf(&b, "| %s | %d |\n", capitalize(total.label), total.count)
	}

	for _, section := range summary.sections {
		fmt.Fprintf(&b, "\n## %s (%d)\n\n", section.title, len(section.entries))

		renderReportSection(&b, section)
	}

	return squeezeBlanks(b.String())
}

// capitalize raises the first letter of a label, the rest is a field name or a
// record kind and is left as it is written
func capitalize(label string) string {
	return strings.ToUpper(label[:1]) + label[1:]
}

// squeezeBlanks collapses the runs of blank lines the sections leave behind,
// a table ends in one and the section that follows opens with another
func squeezeBlanks(report string) string {
	for strings.Contains(report, "\n\n\n") {
		report = strings.ReplaceAll(report, "\n\n\n", "\n\n")
	}

	return report
}

// renderReportSection writes one section as markdown, a section reporting
// something about each record is a table and a bare list of records is a list
func renderReportSection(b *strings.Builder, section reportSection) {
	if len(section.columns) > 0 {
		headers := append([]string{section.noun}, section.columns...)

		fmt.Fprintf(b, "| %s |\n|%s|\n", strings.Join(headers, " | "), strings.Repeat(" --- |", len(headers)))

		for _, entry := range section.entries {
			fmt.Fprintf(b, "| %s |\n", strings.Join(lo.Map(append([]string{entry.ref}, entry.cells...),
				func(cell string, _ int) string { return escapeCell(cell) }), " | "))
		}

		return
	}

	for _, entry := range section.entries {
		renderReportEntry(b, entry)
	}
}

// renderReportEntry writes one record, a record with no field changes is a
// line rather than a heading over an empty table
func renderReportEntry(b *strings.Builder, entry reportEntry) {
	if len(entry.fields) == 0 {
		fmt.Fprintf(b, "- %s\n", strings.TrimSpace(escapeCell(entry.ref)+" "+joinNotes(entry.notes, "→ ")))

		return
	}

	fmt.Fprintf(b, "### %s\n\n| Field | Before | After |\n| --- | --- | --- |\n", entry.ref)

	for _, field := range entry.fields {
		fmt.Fprintf(b, "| %s | %s | %s |\n", field.Field,
			reportValue(field, field.From, "_(unset)_"), reportValue(field, field.To, "_(unset)_"))
	}

	b.WriteString("\n")

	if len(entry.notes) > 0 {
		fmt.Fprintf(b, "Linked controls: %s\n\n", strings.Join(entry.notes, ", "))
	}
}

// renderSummary renders the run for a terminal, the same sections ruled into
// tables so a long value wraps in its cell rather than running off the line
func renderSummary(run runReport) string {
	summary := summarize(run)

	var b strings.Builder

	fmt.Fprintf(&b, "Openlane apply summary\n%s · %s\n\nTOTALS\n",
		summary.at.Format(reportTimeFormat), summary.mode)

	totals := table{headers: []string{"Records", "Count"}, rightmost: true}

	for _, total := range summary.totals {
		totals.add(capitalize(total.label), fmt.Sprint(total.count))
	}

	totals.render(&b)

	for _, section := range summary.sections {
		fmt.Fprintf(&b, "\n%s (%d)\n", strings.ToUpper(section.title), len(section.entries))

		renderSection(&b, section)
	}

	return b.String()
}

// renderSection writes one section, records with field changes get a table
// each under their name, the rest share the section's own
func renderSection(b *strings.Builder, section reportSection) {
	records := table{headers: append([]string{section.noun}, section.columns...)}

	for _, entry := range section.entries {
		if len(entry.fields) == 0 {
			records.add(append([]string{entry.ref}, entry.cells...)...)

			continue
		}

		fmt.Fprintf(b, "\n%s%s\n", entry.ref, joinNotes(entry.notes, " → "))

		fields := table{headers: []string{"Field", "Before", "After"}}

		for _, field := range entry.fields {
			fields.add(field.Field, reportValue(field, field.From, "(unset)"), reportValue(field, field.To, "(unset)"))
		}

		fields.render(b)
	}

	records.render(b)
}

// table is a ruled table sized to its content, columns are given up to the
// width of the report and a cell longer than its column wraps inside it
type table struct {
	headers []string
	rows    [][]string
	// rightmost right-aligns the last column, for a column of counts
	rightmost bool
}

// add appends a row
func (t *table) add(cells ...string) {
	t.rows = append(t.rows, cells)
}

// render writes the table, an empty one writes nothing so a section that holds
// only records with their own tables does not trail an empty frame
func (t *table) render(b *strings.Builder) {
	if len(t.rows) == 0 {
		return
	}

	widths := t.widths()

	t.rule(b, "┌", "┬", "┐", widths)
	t.row(b, t.headers, widths)
	t.rule(b, "├", "┼", "┤", widths)

	for _, row := range t.rows {
		t.row(b, row, widths)
	}

	t.rule(b, "└", "┴", "┘", widths)
}

// widths sizes the columns to their content, then takes width off the widest
// column until the table fits the report, so a long description gives way
// before a column of field names does
func (t *table) widths() []int {
	widths := make([]int, len(t.headers))

	for i, header := range t.headers {
		widths[i] = len([]rune(header))
	}

	for _, row := range t.rows {
		for i, cell := range row {
			widths[i] = max(widths[i], len([]rune(cell)))
		}
	}

	// every column carries its padding and the rule that closes it, and the
	// table opens with one more rule
	framing := (reportCellPadding+1)*len(widths) + 1

	for lo.Sum(widths)+framing > reportWidth {
		widest := 0
		for i, width := range widths {
			if width > widths[widest] {
				widest = i
			}
		}

		if widths[widest] <= reportMinColumn {
			break
		}

		widths[widest]--
	}

	return widths
}

// rule writes a horizontal rule with the given corners
func (t *table) rule(b *strings.Builder, left, join, right string, widths []int) {
	cells := lo.Map(widths, func(width int, _ int) string {
		return strings.Repeat("─", width+reportCellPadding)
	})

	fmt.Fprintf(b, "%s%s%s\n", left, strings.Join(cells, join), right)
}

// row writes one row, a cell too long for its column wraps and the row runs to
// as many lines as its tallest cell
func (t *table) row(b *strings.Builder, cells []string, widths []int) {
	lines := lo.Map(cells, func(cell string, i int) []string {
		return wrap(cell, widths[i])
	})

	height := lo.Max(lo.Map(lines, func(l []string, _ int) int { return len(l) }))

	for line := range height {
		b.WriteString("│")

		for i, width := range widths {
			cell := ""
			if line < len(lines[i]) {
				cell = lines[i][line]
			}

			pad := strings.Repeat(" ", width-len([]rune(cell)))

			if t.rightmost && i == len(widths)-1 {
				fmt.Fprintf(b, " %s%s │", pad, cell)

				continue
			}

			fmt.Fprintf(b, " %s%s │", cell, pad)
		}

		b.WriteString("\n")
	}
}

// wrap breaks a value into lines that fit a column, on spaces where it can and
// mid-word where a single word is longer than the column
func wrap(value string, width int) []string {
	var (
		lines []string
		line  string
	)

	for _, word := range strings.Fields(value) {
		switch {
		case line == "":
			line = word
		case len([]rune(line))+1+len([]rune(word)) <= width:
			line += " " + word
		default:
			lines = append(lines, line)
			line = word
		}

		for len([]rune(line)) > width {
			runes := []rune(line)
			lines = append(lines, string(runes[:width]))
			line = string(runes[width:])
		}
	}

	return append(lines, line)
}

// joinNotes renders the references an entry carries, prefixed when there are any
func joinNotes(notes []string, prefix string) string {
	if len(notes) == 0 {
		return ""
	}

	return prefix + strings.Join(notes, ", ")
}

// reportValue renders one side of a field change, unset is spelled out so an
// added value reads as added rather than as a blank
func reportValue(field engine.FieldDiff, value, unset string) string {
	if value == "" {
		return unset
	}

	value = escapeCell(value)

	if field.Field == reportBodyField {
		value = truncate(value, reportBodyLimit)
	}

	return value
}

// truncate shortens a value to limit characters
func truncate(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}

	return strings.TrimSpace(string(runes[:limit])) + "…"
}

// escapeCell flattens a value onto one line and escapes the pipes that would
// otherwise split a markdown table cell
func escapeCell(value string) string {
	value = strings.Join(strings.Fields(value), " ")

	return strings.ReplaceAll(value, "|", `\|`)
}
