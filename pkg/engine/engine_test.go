package engine

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/Yamashou/gqlgenc/clientv2"
	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vektah/gqlparser/v2/gqlerror"

	"github.com/theopenlane/core/common/enums"
	"github.com/theopenlane/go-client/graphclient"

	"github.com/theopenlane/courier/pkg/controlfile"
)

func remoteFixture() *RemoteState {
	return &RemoteState{
		Controls: []RemoteControl{
			{
				ID:                 "CTL_custom1",
				RefCode:            "CC1.1.3",
				Description:        "Acknowledgment form on hire",
				Category:           "Control Environment",
				Subcategory:        "Integrity and Ethics",
				Status:             "APPROVED",
				CategoryID:         "CC1.1",
				ControlOwnerID:     "GRP_1",
				ControlOwner:       "Security Team",
				DelegateID:         "GRP_2",
				Delegate:           "Compliance Team",
				ReferenceID:        "INT-1",
				AuditorReferenceID: "AUD-1",
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

	// groups are written as their display name, not the ID the record carries
	assert.Equal(t, "Security Team", controls[0].ControlOwner)
	assert.Equal(t, "Compliance Team", controls[0].Delegate)
	assert.Equal(t, "CC1.1", controls[0].CategoryID)
	assert.Equal(t, "INT-1", controls[0].ReferenceID)
	assert.Equal(t, "AUD-1", controls[0].AuditorReferenceID)

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

func TestNewStoreRejectsEscapingMarkdownPath(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "secret.env"), []byte("COURIER_TOKEN=shh\n"), filePerm))

	dir := filepath.Join(root, "store")
	require.NoError(t, os.MkdirAll(dir, dirPerm))

	for _, markdownPath := range []string{"../secret.env", "/etc/passwd", "policies/../../secret.env"} {
		manifest := "- name: Exfil\n  markdownPath: " + markdownPath + "\n"
		require.NoError(t, os.WriteFile(filepath.Join(dir, controlfile.PoliciesFile), []byte(manifest), filePerm))

		_, err := NewStore(dir)
		assert.ErrorIs(t, err, controlfile.ErrUnsafeMarkdownPath, markdownPath)
	}

	// a path inside the store that is not a markdown document is also rejected
	manifest := "- name: Exfil\n  markdownPath: policies/secret.env\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, controlfile.PoliciesFile), []byte(manifest), filePerm))

	_, err := NewStore(dir)
	assert.ErrorIs(t, err, controlfile.ErrUnsafeMarkdownPath)
}

func TestNewStoreNullEntryIsAnError(t *testing.T) {
	dir := t.TempDir()

	// a trailing list dash yields a null entry, which must not dereference
	require.NoError(t, os.WriteFile(filepath.Join(dir, controlfile.PoliciesFile), []byte("- name: A\n-\n"), filePerm))

	_, err := NewStore(dir)
	assert.ErrorIs(t, err, controlfile.ErrSchemaValidation)

	require.NoError(t, os.Remove(filepath.Join(dir, controlfile.PoliciesFile)))
	require.NoError(t, os.WriteFile(filepath.Join(dir, controlfile.ControlsFile), []byte("- refCode: A\n-\n"), filePerm))

	_, err = NewStore(dir)
	assert.ErrorIs(t, err, controlfile.ErrSchemaValidation)
}

func TestNewStoreRejectsUnknownFields(t *testing.T) {
	dir := t.TempDir()

	// a misspelled field must fail rather than silently drop the edit
	require.NoError(t, os.WriteFile(filepath.Join(dir, controlfile.ControlsFile),
		[]byte("- refCode: CC1.1\n  descriptionn: typo\n"), filePerm))

	_, err := NewStore(dir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "descriptionn")
}

func TestBuildPoliciesDisambiguatesCollidingNames(t *testing.T) {
	state := &RemoteState{
		Policies: []RemotePolicy{
			{ID: "PLC_1", Name: "Access Policy"},
			{ID: "PLC_2", Name: "Access-Policy"},
			{ID: "PLC_3", Name: "策略"},
			{ID: "PLC_4", Name: "政策"},
		},
	}

	policies, markdown, err := buildPolicies(state)
	require.NoError(t, err)

	// every policy gets its own document
	assert.Len(t, markdown, len(state.Policies))

	paths := lo.Map(policies, func(p *controlfile.Policy, _ int) string { return p.MarkdownPath })
	assert.Len(t, lo.Uniq(paths), len(state.Policies))

	// names that do not collide keep the plain derived path
	solo, _, err := buildPolicies(&RemoteState{Policies: []RemotePolicy{{ID: "PLC_1", Name: "Access Policy"}}})
	require.NoError(t, err)
	assert.Equal(t, "policies/access-policy.md", solo[0].MarkdownPath)

	// assignment does not depend on the order the API returned
	reversed := &RemoteState{Policies: []RemotePolicy{
		state.Policies[1], state.Policies[0], state.Policies[3], state.Policies[2],
	}}

	_, reversedMarkdown, err := buildPolicies(reversed)
	require.NoError(t, err)
	assert.ElementsMatch(t, lo.Keys(markdown), lo.Keys(reversedMarkdown))
}

func TestChangedControlFields(t *testing.T) {
	remote := remoteFixture().Controls[0]
	doc := buildControls(remoteFixture())[0]

	// a control straight from pull matches its record
	assert.Empty(t, changedControlFields(doc, remote))

	// an empty field is unmanaged, not a request to clear the value
	doc.Title = ""
	assert.Empty(t, changedControlFields(doc, remote))

	// the changed fields are named so a dry run says what will be written
	doc.Title = "New title"
	doc.Category = "New category"
	assert.Equal(t, []string{"title", "category"}, changedControlFields(doc, remote).names())

	// a refCode rename on a matched control is an edit, not a new control
	renamed := buildControls(remoteFixture())[0]
	renamed.RefCode = "CC1.1.9"
	assert.Equal(t, []string{"refCode"}, changedControlFields(renamed, remote).names())

	// without an ID the refCode is the match key, so a difference means create
	renamed.ID = ""
	assert.Empty(t, changedControlFields(renamed, remote))

	// tag order is not a change
	tagged := &controlfile.Control{Tags: []string{"b", "a"}}
	assert.Empty(t, changedControlFields(tagged, RemoteControl{Tags: []string{"a", "b"}}))
	assert.Equal(t, []string{"tags"}, changedControlFields(tagged, RemoteControl{Tags: []string{"a"}}).names())

	// a rich-text description compares against its plain text rendering
	plain := &controlfile.Control{Description: "Hiring Managers evaluate all candidates."}
	rich := RemoteControl{Description: `<div><span>Hiring Managers evaluate all candidates.</span></div>`}
	assert.Empty(t, changedControlFields(plain, rich))

	// the group fields compare by display name, which resolves case-insensitively
	owned := &controlfile.Control{ControlOwner: "security team", Delegate: "compliance team"}
	assert.Empty(t, changedControlFields(owned, RemoteControl{ControlOwner: "Security Team", Delegate: "Compliance Team"}))
	assert.Equal(t, []string{"controlOwner", "delegate"}, changedControlFields(owned, RemoteControl{ControlOwner: "Compliance", Delegate: "Security Team"}).names())

	// the reference fields are managed the same way as the rest
	refs := &controlfile.Control{CategoryID: "CC1.1", ReferenceID: "INT-1", AuditorReferenceID: "AUD-1"}
	assert.Equal(t, []string{"categoryID", "referenceID", "auditorReferenceID"}, changedControlFields(refs, RemoteControl{}).names())
	assert.Empty(t, changedControlFields(refs, RemoteControl{CategoryID: "CC1.1", ReferenceID: "INT-1", AuditorReferenceID: "AUD-1"}))

	// status compares the way the enum parses it, casing is not an edit
	status := &controlfile.Control{Status: "approved"}
	assert.Empty(t, changedControlFields(status, RemoteControl{Status: "APPROVED"}))
	assert.Equal(t, []string{"status"}, changedControlFields(status, RemoteControl{Status: "DRAFT"}).names())
}

// fields builds the changed-field set an update reports, the values on either
// side are not what the status gate reads
func fields(names ...string) fieldChanges {
	var changed fieldChanges

	for _, name := range names {
		changed.record(name, "", "")
	}

	return changed
}

func TestUpdateStatusNeedsApprovalOnDescriptionEdit(t *testing.T) {
	state := &applyState{result: &ApplyResult{}}

	// an edit to what the control says sends it back for approval, whatever
	// the file's own status says, and the move is reported
	status, changed := state.updateStatus("CC1.1", "APPROVED", "APPROVED", fields("description"))
	assert.Equal(t, enums.ControlStatusNeedsApproval, lo.FromPtr(status))
	assert.Equal(t, []string{"description", "status"}, changed.names())

	// the move names the status it writes, so a report can show the edit
	assert.Equal(t, FieldDiff{Field: "status", From: "APPROVED", To: "NEEDS_APPROVAL"}, changed[1])

	// a record already awaiting approval is not reported as a status change
	status, changed = state.updateStatus("CC1.1", "APPROVED", "needs_approval", fields("description"))
	assert.Equal(t, enums.ControlStatusNeedsApproval, lo.FromPtr(status))
	assert.Equal(t, []string{"description"}, changed.names())

	// status is not doubled up when the file already differs from the record
	_, changed = state.updateStatus("CC1.1", "DRAFT", "APPROVED", fields("description", "status"))
	assert.Equal(t, []string{"description", "status"}, changed.names())

	// every other edit keeps the status the file carries
	status, changed = state.updateStatus("CC1.1", "DRAFT", "APPROVED", fields("title"))
	assert.Equal(t, enums.ControlStatusDraft, lo.FromPtr(status))
	assert.Equal(t, []string{"title"}, changed.names())

	// and a record the file gives no status still leaves it alone
	status, _ = state.updateStatus("CC1.1", "", "APPROVED", fields("title"))
	assert.Nil(t, status)

	// a policy's body is the text its own gate watches
	policyStatus, changed := state.updatePolicyStatus("Access Policy", "PUBLISHED", "PUBLISHED", fields("body"))
	assert.Equal(t, enums.DocumentNeedsApproval, lo.FromPtr(policyStatus))
	assert.Equal(t, []string{"body", "status"}, changed.names())

	policyStatus, _ = state.updatePolicyStatus("Access Policy", "PUBLISHED", "PUBLISHED", fields("tags"))
	assert.Equal(t, enums.DocumentPublished, lo.FromPtr(policyStatus))

	// --keep-status writes what the files carry, gate off
	kept := &applyState{keepStatus: true, result: &ApplyResult{}}

	status, changed = kept.updateStatus("CC1.1", "APPROVED", "APPROVED", fields("description"))
	assert.Equal(t, enums.ControlStatusApproved, lo.FromPtr(status))
	assert.Equal(t, []string{"description"}, changed.names())

	policyStatus, changed = kept.updatePolicyStatus("Access Policy", "PUBLISHED", "PUBLISHED", fields("body"))
	assert.Equal(t, enums.DocumentPublished, lo.FromPtr(policyStatus))
	assert.Equal(t, []string{"body"}, changed.names())
}

func TestControlStatusResolution(t *testing.T) {
	state := &applyState{result: &ApplyResult{}}

	// statuses are matched case-insensitively by the enum
	assert.Equal(t, enums.ControlStatusApproved, lo.FromPtr(state.controlStatus("CC1.1", "approved")))
	assert.Equal(t, enums.ControlStatusNotApplicable, lo.FromPtr(state.controlStatus("CC1.1", "NOT_APPLICABLE")))
	assert.Empty(t, state.result.Warnings)

	// an unset status is unmanaged, courier leaves the status in Openlane alone
	assert.Nil(t, state.controlStatus("CC1.1", ""))
	assert.Empty(t, state.result.Warnings)

	// a typo is skipped rather than written to the API as invalid
	assert.Nil(t, state.controlStatus("CC1.1", "aproved"))
	assert.Len(t, state.result.Warnings, 1)
	assert.Contains(t, state.result.Warnings[0], `status "aproved" not recognized`)
}

func TestResolveGroupNames(t *testing.T) {
	state := &RemoteState{
		Controls: []RemoteControl{
			{ID: "CTL_1", ControlOwnerID: "GRP_1", DelegateID: "GRP_2"},
			{ID: "CTL_2", ControlOwnerID: "GRP_gone"},
		},
		Subcontrols: []RemoteSubcontrol{{RemoteControl: RemoteControl{ID: "SCL_1", ControlOwnerID: "GRP_1"}}},
		Groups:      []RemoteGroup{{ID: "GRP_1", DisplayName: "Security Team"}, {ID: "GRP_2", DisplayName: "Compliance Team"}},
	}

	resolveGroupNames(state)

	// the file carries the group display name, the record carries only its ID
	assert.Equal(t, "Security Team", state.Controls[0].ControlOwner)
	assert.Equal(t, "Compliance Team", state.Controls[0].Delegate)
	assert.Equal(t, "Security Team", state.Subcontrols[0].ControlOwner)

	// a group the organization does not have renders as unset
	assert.Empty(t, state.Controls[1].ControlOwner)
}

func TestGroupIDResolution(t *testing.T) {
	groups := []RemoteGroup{{ID: "GRP_1", DisplayName: "Security Team"}}

	assert.Equal(t, "Security Team", groupNamesByID(groups)["GRP_1"])

	state := &applyState{groupIDs: groupIDsByName(groups), result: &ApplyResult{}}

	// the file names the group by display name, casing is not significant
	assert.Equal(t, "GRP_1", lo.FromPtr(state.groupID("controlOwner", "CC1.1", "security team")))
	assert.Empty(t, state.result.Warnings)

	// an unset group is unmanaged, courier leaves the value in Openlane alone
	assert.Nil(t, state.groupID("controlOwner", "CC1.1", ""))
	assert.Empty(t, state.result.Warnings)

	// groups are never created here, a name matching none is skipped with a
	// warning naming the field it came from
	assert.Nil(t, state.groupID("delegate", "CC1.1", "Greg Field"))
	assert.Len(t, state.result.Warnings, 1)
	assert.Contains(t, state.result.Warnings[0], `group "Greg Field" not found, skipping delegate of "CC1.1"`)
}

func TestChangedPolicyFields(t *testing.T) {
	state := remoteFixture()
	remote := state.Policies[0]

	policies, markdown, err := buildPolicies(state)
	require.NoError(t, err)

	fm, body, err := controlfile.SplitPolicyMarkdown(markdown[policies[0].MarkdownPath])
	require.NoError(t, err)

	// a policy straight from pull matches its record
	assert.Empty(t, changedPolicyFields(policies[0], fm, body, remote))

	assert.Equal(t, []string{"body"}, changedPolicyFields(policies[0], fm, body+"\n\nnew paragraph", remote).names())

	// a stored body that differs only by surrounding whitespace is not an edit,
	// the store trims it on read so an untrimmed comparison never converges
	padded := remote
	padded.Details = new(" \n" + lo.FromPtr(remote.Details) + " \n\n\n")
	assert.Empty(t, changedPolicyFields(policies[0], fm, body, padded))

	// the server applies the frontmatter over the manifest, so a frontmatter
	// edit must be detected rather than silently dropped
	retagged := fm
	retagged.Tags = append(slices.Clone(fm.Tags), "new-tag")
	assert.Equal(t, []string{"tags"}, changedPolicyFields(policies[0], retagged, body, remote).names())

	renamed := fm
	renamed.Title = "Renamed In Frontmatter"
	assert.Equal(t, []string{"name"}, changedPolicyFields(policies[0], renamed, body, remote).names())

	// the server bumps revision after every write, so a revision-only
	// difference must not trigger an update or apply never converges
	edited := fm
	edited.Revision = "v2.0.0"
	assert.Empty(t, changedPolicyFields(policies[0], edited, body, remote))
}

func TestFlattenTargets(t *testing.T) {
	// output is sorted so a dry run reads the same across runs
	assert.Equal(t,
		[]string{"ISO 27001: A.5.1", "SOC 2: CC1.1", "SOC 2: CC6.2"},
		flattenTargets(controlfile.MappedControls{"SOC 2": {"CC6.2", "CC1.1"}, "ISO 27001": {"A.5.1"}}))

	assert.Empty(t, flattenTargets(nil))
}

func TestMissingTargets(t *testing.T) {
	existing := controlfile.MappedControls{"SOC 2": {"CC1.1", "CC6.2"}}

	assert.Empty(t, missingTargets(controlfile.MappedControls{"SOC 2": {"CC1.1"}}, existing))

	// refCodes resolve case-insensitively, so casing alone is not a new target
	assert.Empty(t, missingTargets(controlfile.MappedControls{"SOC 2": {"cc1.1"}}, existing))

	assert.Equal(t,
		controlfile.MappedControls{"SOC 2": {"CC9.9"}},
		missingTargets(controlfile.MappedControls{"SOC 2": {"CC1.1", "CC9.9"}}, existing))

	// a framework with nothing mapped yet is entirely missing
	assert.Equal(t,
		controlfile.MappedControls{"ISO 27001": {"A.5.1"}},
		missingTargets(controlfile.MappedControls{"ISO 27001": {"A.5.1"}}, existing))
}

func TestOwnedMappings(t *testing.T) {
	state := &RemoteState{Mappings: []RemoteMapping{
		{
			ID:     "MPC_own",
			Source: importedSource,
			From:   []RemoteRef{{ID: "CTL_1"}},
			To:     []RemoteRef{{RefCode: "CC1.1", Framework: "SOC 2"}},
		},
		{
			ID:     "MPC_manual",
			Source: "MANUAL",
			From:   []RemoteRef{{ID: "CTL_1"}},
			To:     []RemoteRef{{RefCode: "A.5.1", Framework: "ISO 27001"}},
		},
		{
			ID:     "MPC_mixed",
			Source: importedSource,
			From:   []RemoteRef{{ID: "CTL_2"}},
			To:     []RemoteRef{{RefCode: "CC1.1", Framework: "SOC 2"}, {RefCode: "A.5.1", Framework: "ISO 27001"}},
		},
	}}

	owned := ownedMappings(state)

	// courier extends the record it created for that control and framework
	assert.Equal(t, "MPC_own", owned[mappingKey("CTL_1", "SOC 2")].id)
	assert.Equal(t, []string{"CC1.1"}, owned[mappingKey("CTL_1", "SOC 2")].targets)

	// records courier did not create are never edited
	assert.NotContains(t, owned, mappingKey("CTL_1", "ISO 27001"))

	// a record spanning frameworks is not one courier wrote, so it is left alone
	assert.NotContains(t, owned, mappingKey("CTL_2", "SOC 2"))

	// subcontrol targets group under their framework like controls do
	subs := &RemoteState{Mappings: []RemoteMapping{{
		ID:     "MPC_sub",
		Source: importedSource,
		From:   []RemoteRef{{ID: "CTL_3"}},
		To:     []RemoteRef{{RefCode: "CC1.1-POF1", Framework: "SOC 2", Subcontrol: true}},
	}}}

	assert.Equal(t, "MPC_sub", ownedMappings(subs)[mappingKey("CTL_3", "SOC 2")].id)
}

func TestAddGroupedRefsIncludesSubcontrols(t *testing.T) {
	grouped := controlfile.MappedControls{}
	addGroupedRefs(grouped, []RemoteRef{
		{RefCode: "CC1.1", Framework: "SOC 2"},
		{RefCode: "CC1.1-POF1", Framework: "SOC 2", Subcontrol: true},
		{RefCode: "OWN-1"},
	})

	// the file names a reference, apply resolves whether it is a subcontrol
	assert.Equal(t, controlfile.MappedControls{
		"SOC 2":                        {"CC1.1", "CC1.1-POF1"},
		controlfile.CustomFrameworkKey: {"OWN-1"},
	}, grouped)
}

func TestIsAlreadyExists(t *testing.T) {
	coded := func(code string) error {
		return &clientv2.ErrorResponse{GqlErrors: &gqlerror.List{
			{Message: "control already exists in the system", Extensions: map[string]any{"code": code}},
		}}
	}

	assert.True(t, isAlreadyExists(coded(alreadyExistsCode)))

	// CONFLICT carries the same wording but is a real failure
	assert.False(t, isAlreadyExists(coded("CONFLICT")))

	assert.False(t, isAlreadyExists(nil))
	assert.True(t, isAlreadyExists(errors.New("control already exists")))
	assert.False(t, isAlreadyExists(errors.New("connection reset")))
}

func TestPaginateStalls(t *testing.T) {
	// hasNextPage with no cursor must stop rather than refetch page one forever
	calls := 0
	err := paginate(func(_ *string) (*graphclient.GetControls_Controls_PageInfo, error) {
		calls++

		return &graphclient.GetControls_Controls_PageInfo{HasNextPage: true}, nil
	})

	assert.ErrorIs(t, err, ErrPaginationStalled)
	assert.Equal(t, 1, calls)
}

func TestLoadSettingsDefaults(t *testing.T) {
	settings, err := LoadSettings(filepath.Join(t.TempDir(), "absent.yaml"), nil)
	assert.Error(t, err)

	settings, err = LoadSettings("", nil)
	require.NoError(t, err)
	assert.Equal(t, DefaultHost, settings.Host)
	assert.Equal(t, DefaultDir, settings.Dir)
}

func TestNewClientRequiresHostAndToken(t *testing.T) {
	_, err := NewClient(Config{Host: DefaultHost})
	assert.ErrorIs(t, err, ErrMissingToken)

	_, err = NewClient(Config{Token: "tolp_x"})
	assert.ErrorIs(t, err, ErrMissingHost)
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

func TestMappedTargets(t *testing.T) {
	targets := mappedTargets(remoteFixture())

	// targets are the to side of every mapping the control is on the from side of
	assert.Equal(t, controlfile.MappedControls{"SOC 2": {"CC1.1"}}, targets["CTL_custom1"])
	assert.Empty(t, targets["CTL_custom2"])
}
