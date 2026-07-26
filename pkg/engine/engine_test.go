package engine

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/theopenlane/courier/pkg/controlfile"
)

func remoteFixture() *RemoteState {
	return &RemoteState{
		Controls: []RemoteControl{
			{
				ID:          "CTL_custom1",
				RefCode:     "CC1.1.3",
				Description: "Acknowledgment form on hire",
				Category:    "Control Environment",
				Subcategory: "Integrity and Ethics",
			},
			{
				ID:      "CTL_custom2",
				RefCode: "CC1.1.1",
				Title:   "Second control",
			},
		},
		Mappings: []RemoteMapping{
			{
				ID:   "MPC_1",
				From: []RemoteRef{{ID: "CTL_custom1", RefCode: "CC1.1.3"}},
				To:   []RemoteRef{{ID: "CTL_soc2", RefCode: "CC1.1"}},
			},
		},
		Policies: []RemotePolicy{
			{
				ID:       "PLC_1",
				Name:     "Application Security Policy",
				KindName: new("Security"),
				Details:  new("# Purpose\n\nSecure all the things."),
				Tags:     []string{"application"},
				Controls: []RemoteRef{{ID: "CTL_soc2b", RefCode: "CC6.2"}},
			},
		},
	}
}

func workspaceFromState(t *testing.T, state *RemoteState) *Workspace {
	t.Helper()

	controls := BuildControls(state)

	policies, markdown, err := BuildPolicies(state)
	require.NoError(t, err)

	return &Workspace{Controls: controls, Policies: policies, PolicyMarkdown: markdown}
}

func TestBuildControlsDerivesMappedControls(t *testing.T) {
	controls := BuildControls(remoteFixture())
	require.Len(t, controls, 2)

	// controls are written in API order with data verbatim
	assert.Equal(t, "CC1.1.3", controls[0].RefCode)
	assert.Equal(t, []string{"CC1.1"}, controls[0].MappedControls)

	assert.Equal(t, "CC1.1.1", controls[1].RefCode)
	assert.Empty(t, controls[1].MappedControls)
}

func TestBuildPolicies(t *testing.T) {
	policies, markdown, err := BuildPolicies(remoteFixture())
	require.NoError(t, err)
	require.Len(t, policies, 1)

	policy := policies[0]
	assert.Equal(t, "Application Security Policy", policy.Name)
	assert.Equal(t, "Security", policy.PolicyType)
	assert.Equal(t, []string{"CC6.2"}, policy.MappedControls)
	assert.Equal(t, "policies/application-security-policy.md", policy.MarkdownPath)

	doc := markdown[policy.MarkdownPath]
	require.NotNil(t, doc)

	fm, body, err := controlfile.SplitPolicyMarkdown(doc)
	require.NoError(t, err)
	assert.Equal(t, policy.Name, fm.Title)
	assert.Equal(t, "# Purpose\n\nSecure all the things.", body)
}

func TestComputePlanNoChanges(t *testing.T) {
	state := remoteFixture()

	plan, err := ComputePlan(workspaceFromState(t, state), state)
	require.NoError(t, err)

	assert.False(t, plan.HasChanges())
	assert.Empty(t, plan.DriftControls)
	assert.Empty(t, plan.DriftMappings)
	assert.Empty(t, plan.DriftPolicies)
}

func TestComputePlanCreateUpdateDrift(t *testing.T) {
	state := remoteFixture()
	ws := workspaceFromState(t, state)

	// drop the second control from the workspace so it drifts, edit the first,
	// and add a brand new one with mappings
	ws.Controls = lo.Filter(ws.Controls, func(c *controlfile.Control, _ int) bool { return c.RefCode != "CC1.1.1" })
	ws.Controls[0].Description = "Updated description"
	ws.Controls[0].MappedControls = append(ws.Controls[0].MappedControls, "CC1.5")
	ws.Controls = append(ws.Controls, &controlfile.Control{
		RefCode:        "CC9.9.9",
		Description:    "Brand new control",
		MappedControls: []string{"CC9.1"},
	})

	plan, err := ComputePlan(ws, state)
	require.NoError(t, err)

	require.Len(t, plan.CreateControls, 1)
	assert.Equal(t, "CC9.9.9", plan.CreateControls[0].Doc.RefCode)

	require.Len(t, plan.UpdateControls, 1)
	require.Len(t, plan.UpdateControls[0].Diffs, 1)
	assert.Equal(t, "description", plan.UpdateControls[0].Diffs[0].Field)

	// one add for the edited control, one for the created control
	require.Len(t, plan.MappingAdds, 2)
	byRefCode := lo.SliceToMap(plan.MappingAdds, func(a MappingAdd) (string, MappingAdd) { return a.RefCode, a })
	assert.Equal(t, []string{"CC1.5"}, byRefCode["CC1.1.3"].Targets)
	assert.Equal(t, []string{"CC9.1"}, byRefCode["CC9.9.9"].Targets)

	require.Len(t, plan.DriftControls, 1)
	assert.Equal(t, "CC1.1.1", plan.DriftControls[0].RefCode)
}

func TestComputePlanMatchByRefCodeAdoptsControl(t *testing.T) {
	state := remoteFixture()
	ws := workspaceFromState(t, state)

	// strip IDs so matching falls back to refCode
	for _, c := range ws.Controls {
		c.ID = ""
	}

	plan, err := ComputePlan(ws, state)
	require.NoError(t, err)

	assert.Empty(t, plan.CreateControls)
	assert.Empty(t, plan.UpdateControls)
	assert.Empty(t, plan.DriftControls)
}

func TestComputePlanMappingRemovalIsDrift(t *testing.T) {
	state := remoteFixture()
	ws := workspaceFromState(t, state)

	for _, c := range ws.Controls {
		c.MappedControls = nil
	}

	plan, err := ComputePlan(ws, state)
	require.NoError(t, err)

	assert.Empty(t, plan.MappingAdds)
	require.Len(t, plan.DriftMappings, 1)
	assert.Equal(t, "CC1.1.3", plan.DriftMappings[0].RefCode)
	assert.Equal(t, []string{"CC1.1"}, plan.DriftMappings[0].Targets)
}

func TestComputePlanPolicyChanges(t *testing.T) {
	state := remoteFixture()
	ws := workspaceFromState(t, state)

	policy := ws.Policies[0]
	policy.PolicyType = "Operational"
	policy.MappedControls = append(policy.MappedControls, "CC9.1")

	markdown, err := controlfile.MarshalPolicyMarkdown(controlfile.Frontmatter{Title: policy.Name}, "# Purpose\n\nRewritten body.")
	require.NoError(t, err)
	ws.PolicyMarkdown[policy.MarkdownPath] = markdown

	ws.Policies = append(ws.Policies, &controlfile.Policy{
		Name:           "Availability Policy",
		PolicyType:     "Operational",
		MappedControls: []string{"A1.1"},
	})

	newMD, err := controlfile.MarshalPolicyMarkdown(controlfile.Frontmatter{Title: "Availability Policy"}, "body")
	require.NoError(t, err)
	ws.PolicyMarkdown[controlfile.PolicyMarkdownPath("Availability Policy")] = newMD

	plan, err := ComputePlan(ws, state)
	require.NoError(t, err)

	require.Len(t, plan.CreatePolicies, 1)
	assert.Equal(t, "Availability Policy", plan.CreatePolicies[0].Policy.Name)

	require.Len(t, plan.UpdatePolicies, 1)
	update := plan.UpdatePolicies[0]
	assert.True(t, update.BodyChanged)
	assert.Equal(t, []string{"CC9.1"}, update.AddControls)
	require.Len(t, update.Diffs, 1)
	assert.Equal(t, "policyType", update.Diffs[0].Field)
}

func TestComputePlanBodyComparisonMatchesServerSanitization(t *testing.T) {
	state := remoteFixture()
	// the server stores entity-escaped details, a raw-authored file body with
	// the same content must not register as a change
	state.Policies[0].Details = new("Access &amp; Termination &#34;policy&#34; body.")

	ws := workspaceFromState(t, state)
	policy := ws.Policies[0]

	markdown, err := controlfile.MarshalPolicyMarkdown(
		controlfile.Frontmatter{Title: policy.Name},
		`Access & Termination "policy" body.`,
	)
	require.NoError(t, err)
	ws.PolicyMarkdown[policy.MarkdownPath] = markdown

	plan, err := ComputePlan(ws, state)
	require.NoError(t, err)

	assert.Empty(t, plan.UpdatePolicies)
}

func TestWorkspaceRoundTripThroughDisk(t *testing.T) {
	dir := t.TempDir()
	state := remoteFixture()

	controls := BuildControls(state)
	policies, markdown, err := BuildPolicies(state)
	require.NoError(t, err)

	written, removed, err := WriteWorkspace(dir, controls, policies, markdown)
	require.NoError(t, err)
	assert.Len(t, written, 3)
	assert.Empty(t, removed)

	// second write is a no-op
	written, removed, err = WriteWorkspace(dir, controls, policies, markdown)
	require.NoError(t, err)
	assert.Empty(t, written)
	assert.Empty(t, removed)

	ws, err := LoadWorkspace(dir)
	require.NoError(t, err)

	plan, err := ComputePlan(ws, state)
	require.NoError(t, err)
	assert.False(t, plan.HasChanges())

	// dropping a policy removes its markdown document
	_, removed, err = WriteWorkspace(dir, controls, nil, map[string][]byte{})
	require.NoError(t, err)
	assert.Equal(t, []string{"policies/application-security-policy.md"}, removed)
}

func TestFormatCanonicalizes(t *testing.T) {
	dir := t.TempDir()

	raw := "- refCode: CC1.1.3\n  tags: [b, a]\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, controlfile.ControlsFile), []byte(raw), filePerm))

	result, err := Format(dir, false)
	require.NoError(t, err)
	assert.Equal(t, []string{controlfile.ControlsFile}, result.Changed)

	// formatting fixes YAML style only, the data is untouched
	ws, err := LoadWorkspace(dir)
	require.NoError(t, err)
	require.Len(t, ws.Controls, 1)
	assert.Equal(t, []string{"b", "a"}, ws.Controls[0].Tags)

	// a second format run is a no-op
	result, err = Format(dir, true)
	require.NoError(t, err)
	assert.Empty(t, result.Changed)
}

func TestLoadWorkspaceMissingMarkdown(t *testing.T) {
	dir := t.TempDir()

	manifest := "- name: Lonely Policy\n  markdownPath: policies/lonely.md\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, controlfile.PoliciesFile), []byte(manifest), filePerm))

	_, err := LoadWorkspace(dir)
	assert.ErrorIs(t, err, ErrMissingMarkdown)
}

func TestLoadSettingsPrecedence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	require.NoError(t, os.WriteFile(path, []byte("host: https://file.example.com\ntoken: file-token\ndir: compliance\n"), filePerm))

	settings, err := LoadSettings(path, nil)
	require.NoError(t, err)
	assert.Equal(t, "https://file.example.com", settings.Host)
	assert.Equal(t, "file-token", settings.Token)
	assert.Equal(t, "compliance", settings.Dir)

	t.Setenv("COURIER_TOKEN", "env-token")

	settings, err = LoadSettings(path, nil)
	require.NoError(t, err)
	assert.Equal(t, "env-token", settings.Token)
	assert.Equal(t, "https://file.example.com", settings.Host)

	// an explicit but missing config file is an error
	_, err = LoadSettings(filepath.Join(dir, "missing.yaml"), nil)
	assert.Error(t, err)
}
