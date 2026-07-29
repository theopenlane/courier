package engine

import (
	"os"
	"path/filepath"
	"testing"

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
				To:   []RemoteRef{{ID: "CTL_soc2", RefCode: "CC1.1", Framework: "SOC 2"}},
			},
		},
		Policies: []RemotePolicy{
			{
				ID:       "PLC_1",
				Name:     "Application Security Policy",
				KindName: new("Security"),
				Details:  new("# Purpose\n\nSecure all the things."),
				Tags:     []string{"application"},
				Controls: []RemoteRef{{ID: "CTL_soc2b", RefCode: "CC6.2", Framework: "SOC 2"}},
			},
		},
	}
}

func storeFromState(t *testing.T, state *RemoteState) *Store {
	t.Helper()

	controls := buildControls(state)

	policies, markdown, err := buildPolicies(state)
	require.NoError(t, err)

	return &Store{Controls: controls, Policies: policies, PolicyMarkdown: markdown}
}

func TestBuildControlsDerivesMappedControls(t *testing.T) {
	controls := buildControls(remoteFixture())
	require.Len(t, controls, 2)

	// controls are written in API order with data verbatim
	assert.Equal(t, "CC1.1.3", controls[0].RefCode)
	assert.Equal(t, controlfile.MappedControls{"SOC 2": {"CC1.1"}}, controls[0].MappedControls)

	assert.Equal(t, "CC1.1.1", controls[1].RefCode)
	assert.Empty(t, controls[1].MappedControls)
}

func TestBuildPolicies(t *testing.T) {
	policies, markdown, err := buildPolicies(remoteFixture())
	require.NoError(t, err)
	require.Len(t, policies, 1)

	policy := policies[0]
	assert.Equal(t, "Application Security Policy", policy.Name)
	assert.Equal(t, "Security", policy.PolicyType)
	assert.Equal(t, "policies/application-security-policy.md", policy.MarkdownPath)

	doc := markdown[policy.MarkdownPath]
	require.NotNil(t, doc)

	fm, body, err := controlfile.SplitPolicyMarkdown(doc)
	require.NoError(t, err)
	assert.Equal(t, policy.Name, fm.Title)
	assert.Equal(t, "PLC_1", fm.OpenlaneID)
	assert.Equal(t, controlfile.MappedControls{"SOC 2": {"CC6.2"}}, fm.Satisfies)
	assert.Equal(t, "# Purpose\n\nSecure all the things.", body)
}

func TestStoreRoundTripThroughDisk(t *testing.T) {
	dir := t.TempDir()
	state := remoteFixture()

	var written, removed []string

	for _, spec := range scoped(AllKinds()) {
		rendered, err := spec.build(state)
		require.NoError(t, err)

		w, r, err := writeKindFiles(dir, rendered)
		require.NoError(t, err)

		written = append(written, w...)
		removed = append(removed, r...)
	}

	assert.Len(t, written, 3)
	assert.Empty(t, removed)

	// second write is a no-op
	for _, spec := range scoped(AllKinds()) {
		rendered, err := spec.build(state)
		require.NoError(t, err)

		w, r, err := writeKindFiles(dir, rendered)
		require.NoError(t, err)
		assert.Empty(t, w)
		assert.Empty(t, r)
	}

	store, err := NewStore(dir)
	require.NoError(t, err)
	require.Len(t, store.Controls, 2)
	require.Len(t, store.Policies, 1)

	// dropping a policy removes its markdown document
	state.Policies = nil

	rendered, err := buildPoliciesKind(state)
	require.NoError(t, err)

	_, removed, err = writeKindFiles(dir, rendered)
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
	store, err := NewStore(dir)
	require.NoError(t, err)
	require.Len(t, store.Controls, 1)
	assert.Equal(t, []string{"b", "a"}, store.Controls[0].Tags)

	// a second format run is a no-op
	result, err = Format(dir, true)
	require.NoError(t, err)
	assert.Empty(t, result.Changed)
}

func TestNewStoreMissingMarkdown(t *testing.T) {
	dir := t.TempDir()

	manifest := "- name: Lonely Policy\n  markdownPath: policies/lonely.md\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, controlfile.PoliciesFile), []byte(manifest), filePerm))

	_, err := NewStore(dir)
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

func TestBuildPoliciesConvertsRichTextToMarkdown(t *testing.T) {
	state := remoteFixture()
	state.Policies[0].Details = new(`<div class="slate-editor"><h2 class="slate-h2">Purpose</h2><div class="slate-p"><span>Keep systems safe.</span></div><ol><li><span>First rule.</span></li></ol></div>`)

	_, markdown, err := buildPolicies(state)
	require.NoError(t, err)

	_, body, err := controlfile.SplitPolicyMarkdown(markdown["policies/application-security-policy.md"])
	require.NoError(t, err)

	assert.Contains(t, body, "## Purpose")
	assert.Contains(t, body, "Keep systems safe.")
	assert.Contains(t, body, "1. First rule.")

	// markdown authored content passes through verbatim
	state.Policies[0].Details = new("## Purpose\n\nKeep systems safe.")

	_, markdown, err = buildPolicies(state)
	require.NoError(t, err)

	_, body, err = controlfile.SplitPolicyMarkdown(markdown["policies/application-security-policy.md"])
	require.NoError(t, err)
	assert.Equal(t, "## Purpose\n\nKeep systems safe.", body)
}

func TestPlainTextStripsRenderedHTML(t *testing.T) {
	slate := `<div class="slate-editor"><div data-slate-type="p"><span data-slate-string="true">Hiring Managers evaluate all candidates.</span></div></div>`

	assert.Equal(t, "Hiring Managers evaluate all candidates.", plainText(slate))

	// plain values pass through unchanged, including newlines
	assert.Equal(t, "line one\nline two", plainText("line one\nline two"))

	// idempotent so exported values never diff against re-stripped remote values
	assert.Equal(t, plainText(slate), plainText(plainText(slate)))
}

func TestSelectKinds(t *testing.T) {
	assert.Equal(t, AllKinds(), SelectKinds(nil))
	assert.Equal(t, []Kind{KindControls}, SelectKinds(map[Kind]bool{KindControls: true}))
	assert.Equal(t, []Kind{KindPolicies}, SelectKinds(map[Kind]bool{KindPolicies: true}))
	// registry order holds regardless of selection order
	assert.Equal(t, AllKinds(), SelectKinds(map[Kind]bool{KindPolicies: true, KindControls: true}))
}

func TestMatchRemote(t *testing.T) {
	byID := map[string]RemoteControl{"CTL_1": {ID: "CTL_1", RefCode: "A"}}
	byRefCode := map[string]RemoteControl{"A": {ID: "CTL_1", RefCode: "A"}}
	idOf := func(rc RemoteControl) string { return rc.ID }

	_, ok := matchRemote("CTL_1", "", byID, byRefCode, map[string]struct{}{}, idOf)
	assert.True(t, ok)

	// refCode fallback matches the value as written, the API owns any folding
	_, ok = matchRemote("", "A", byID, byRefCode, map[string]struct{}{}, idOf)
	assert.True(t, ok)

	// consumed records are not matched twice
	_, ok = matchRemote("", "A", byID, byRefCode, map[string]struct{}{"CTL_1": {}}, idOf)
	assert.False(t, ok)
}

func TestMissingMappedRefs(t *testing.T) {
	state := remoteFixture()
	store := storeFromState(t, state)

	// everything in the files, nothing missing
	assert.Empty(t, missingMappedRefs(store, state))

	// dropping the mapping from the files reports it as missing
	for _, doc := range store.Controls {
		doc.MappedControls = nil
	}

	assert.Equal(t, controlfile.MappedControls{"SOC 2": {"CC1.1"}}, missingMappedRefs(store, state))
}
