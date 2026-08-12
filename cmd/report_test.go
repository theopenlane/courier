package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gotest.tools/v3/assert"

	"github.com/theopenlane/courier/pkg/engine"
)

func reportFixture() *engine.ApplyResult {
	return &engine.ApplyResult{
		CreatedControls: []engine.Change{{Ref: "JH-07"}, {Ref: "JH-09"}},
		UpdatedControls: []engine.Change{{
			Ref:    "CC1.1",
			Detail: []string{"title", "controlOwner", "description"},
			Fields: []engine.FieldDiff{
				{Field: "title", From: "Code of conduct", To: "Code of Conduct"},
				{Field: "controlOwner", From: "", To: "Security Team"},
				{Field: "description", From: "Short.", To: strings.Repeat("long ", 100) + "end."},
			},
		}},
		UnchangedControls: 12,
		UpdatedPolicies: []engine.Change{{
			Ref:    "Access Policy",
			Detail: []string{"status", "SOC2: CC6.1"},
			Fields: []engine.FieldDiff{{Field: "status", From: "PUBLISHED", To: "NEEDS_APPROVAL"}},
		}},
		Warnings: []string{`group "Ops" not found`},
	}
}

func reportTime() time.Time {
	return time.Date(2026, 8, 4, 9, 30, 0, 0, time.UTC)
}

func reportRun(dryRun bool) runReport {
	return runReport{
		result: reportFixture(),
		archivable: &engine.ArchivableResult{
			Controls: []engine.ArchivableControl{
				{RefCode: "JH-99", Title: "Retired control", Status: "APPROVED"},
				{RefCode: "1.2", Title: "Retired subcontrol", Status: "DRAFT", Subcontrol: true, ParentRefCode: "JH-98"},
			},
			TotalRemote: 40,
		},
		dryRun: dryRun,
		at:     reportTime(),
	}
}

func TestRenderReport(t *testing.T) {
	report := renderReport(reportRun(false))

	assert.Assert(t, strings.Contains(report, "2026-08-04 09:30 UTC · changes applied to Openlane"))

	// created records are listed on their own, they have no before side
	assert.Assert(t, strings.Contains(report, "## Controls created (2)\n\n- JH-07\n- JH-09\n"))

	// an update reports every changed field with the value on either side, and
	// a value the record did not carry reads as added rather than blank
	assert.Assert(t, strings.Contains(report, "### CC1.1\n\n| Field | Before | After |"))
	assert.Assert(t, strings.Contains(report, "| title | Code of conduct | Code of Conduct |"))
	assert.Assert(t, strings.Contains(report, "| controlOwner | _(unset)_ | Security Team |"))

	// a description renders in full, cutting off what a control now says would
	// leave the summary unreviewable
	assert.Assert(t, strings.Contains(report, "long end. |"))

	// the mapping targets on a policy update are not fields, they are listed
	assert.Assert(t, strings.Contains(report, "| status | PUBLISHED | NEEDS_APPROVAL |"))
	assert.Assert(t, strings.Contains(report, "Linked controls: SOC2: CC6.1"))

	// the controls Openlane holds that the files no longer do, subcontrols
	// named under their parent
	assert.Assert(t, strings.Contains(report, "## Controls to remove (2)"))
	assert.Assert(t, strings.Contains(report, "| Control | Title | Status |"))
	assert.Assert(t, strings.Contains(report, "| JH-99 | Retired control | APPROVED |"))
	assert.Assert(t, strings.Contains(report, "| JH-98/1.2 | Retired subcontrol | DRAFT |"))

	assert.Assert(t, strings.Contains(report, "## Warnings (1)"))

	// sections with nothing in them are left out
	assert.Assert(t, !strings.Contains(report, "## Errors"))
	assert.Assert(t, !strings.Contains(report, "## Policies created"))

	// the totals cover the records a run left alone, which the sections do not
	assert.Assert(t, strings.Contains(report, "| Controls unchanged | 12 |"))
}

func TestRenderSummary(t *testing.T) {
	summary := renderSummary(reportRun(true))

	// the terminal report rules the same sections into tables
	assert.Assert(t, strings.Contains(summary, "2026-08-04 09:30 UTC · dry run, nothing was written"))
	assert.Assert(t, strings.Contains(summary, "CONTROLS CREATED (2)"))
	assert.Assert(t, strings.Contains(summary, "│ JH-07"))
	assert.Assert(t, strings.Contains(summary, "│ Field"))
	assert.Assert(t, strings.Contains(summary, "│ Code of conduct │ Code of Conduct"))
	assert.Assert(t, strings.Contains(summary, "│ (unset)"))
	assert.Assert(t, strings.Contains(summary, "Access Policy → SOC2: CC6.1"))
	assert.Assert(t, strings.Contains(summary, "│ Controls unchanged │"))

	// a section names what one row of it is, rather than calling them all records
	assert.Assert(t, strings.Contains(summary, "│ Warning"))

	assert.Assert(t, strings.Contains(summary, "CONTROLS TO REMOVE (2)"))
	assert.Assert(t, strings.Contains(summary, "│ JH-99"))
	assert.Assert(t, strings.Contains(summary, "│ JH-98/1.2"))
	assert.Assert(t, strings.Contains(summary, "│ Retired subcontrol"))

	// the archivable count sits with the totals, it is a decision about the
	// inventory rather than something the run did
	assert.Assert(t, strings.Contains(summary, "│ Controls to remove"))

	// none of the markdown markup leaks into it
	assert.Assert(t, !strings.Contains(summary, "|"))
	assert.Assert(t, !strings.Contains(summary, "#"))
}

func TestRenderEmptyRun(t *testing.T) {
	// a run that changed nothing still reports its totals, in both formats
	empty := runReport{result: &engine.ApplyResult{}, dryRun: true, at: reportTime()}

	assert.Assert(t, strings.Contains(renderReport(empty), "| Controls created | 0 |"))
	assert.Assert(t, strings.Contains(renderSummary(empty), "│ Controls created"))

	// nothing to archive leaves the section out rather than ruling an empty one
	assert.Assert(t, !strings.Contains(renderReport(empty), "Controls to remove"))
	assert.Assert(t, !strings.Contains(renderSummary(empty), "CONTROLS TO REMOVE"))
}

func TestWriteReport(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "report.md")

	assert.NilError(t, writeReport(path, "# report\n"))

	written, err := os.ReadFile(path) //nolint:gosec // the path is the one the test just wrote
	assert.NilError(t, err)
	assert.Equal(t, "# report\n", string(written))

	// a re-run refreshes the file it already wrote
	assert.NilError(t, writeReport(path, "# newer\n"))

	written, err = os.ReadFile(path) //nolint:gosec // the path is the one the test just wrote
	assert.NilError(t, err)
	assert.Equal(t, "# newer\n", string(written))
}

func TestReportValue(t *testing.T) {
	title := engine.FieldDiff{Field: "title"}

	// a cell is one line, a pipe in the value would otherwise split the row
	assert.Equal(t, `a \| b`, reportValue(title, "a\n| b", "(unset)"))

	// a policy body is the one value cut short, it is a whole document and the
	// point is that it changed
	body := engine.FieldDiff{Field: reportBodyField}
	long := reportValue(body, strings.Repeat("word ", 200), "(unset)")
	assert.Assert(t, len([]rune(long)) <= reportBodyLimit+1)
	assert.Assert(t, strings.HasSuffix(long, "…"))

	// every other field is rendered whole
	assert.Equal(t, 1000, len(reportValue(title, strings.Repeat("x", 1000), "(unset)")))
}
