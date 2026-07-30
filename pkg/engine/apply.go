package engine

import (
	"context"
	"fmt"
	"io"
	"os"
	"slices"

	"github.com/99designs/gqlgen/graphql"
	"github.com/samber/lo"

	"github.com/theopenlane/core/common/enums"
	"github.com/theopenlane/core/pkg/logx"
	"github.com/theopenlane/core/pkg/objects/storage"
	"github.com/theopenlane/go-client/graphclient"

	"github.com/theopenlane/courier/pkg/controlfile"
)

// Change is one record apply wrote, or would write on a dry run
type Change struct {
	// Ref is the control refCode or the policy name
	Ref string `json:"ref"`
	// Detail is the managed fields that differ, or the mapping targets added,
	// empty when the record is created outright
	Detail []string `json:"detail,omitempty"`
}

// ApplyResult summarizes what an apply changed in the API
type ApplyResult struct {
	// CreatedControls are the controls created, in order
	CreatedControls []Change `json:"createdControls,omitempty"`
	// UpdatedControls are the controls updated, in order, with the fields that differ
	UpdatedControls []Change `json:"updatedControls,omitempty"`
	// UnchangedControls counts controls that already match Openlane
	UnchangedControls int `json:"unchangedControls"`
	// CreatedMappings are the mappings created, in order, with the targets added
	CreatedMappings []Change `json:"createdMappings,omitempty"`
	// UpdatedMappings are the mappings courier already owned and extended
	UpdatedMappings []Change `json:"updatedMappings,omitempty"`
	// CreatedPolicies are the policies created, in order
	CreatedPolicies []Change `json:"createdPolicies,omitempty"`
	// UpdatedPolicies are the policies updated, in order, with the fields that differ
	UpdatedPolicies []Change `json:"updatedPolicies,omitempty"`
	// UnchangedPolicies counts policies that already match Openlane
	UnchangedPolicies int `json:"unchangedPolicies"`
	// Warnings are non-fatal issues such as mapped control refCodes that
	// could not be resolved and were skipped
	Warnings []string `json:"warnings,omitempty"`
	// Errors are per-record failures, the run continues past them so one
	// rejected record does not abort the batch
	Errors []string `json:"errors,omitempty"`
}

// ApplyOptions configures an apply run
type ApplyOptions struct {
	// DryRun reports what apply would change without writing anything
	DryRun bool
}

// pendingID stands in for a control a dry run would create, so mapping targets
// that depend on it are counted rather than reported as unresolved
const pendingID = "<pending>"

// targetIDs are the resolved mapping targets, the API keeps controls and
// subcontrols in separate edge sets
type targetIDs struct {
	controls    []string
	subcontrols []string
}

// empty reports whether nothing resolved
func (t targetIDs) empty() bool {
	return len(t.controls)+len(t.subcontrols) == 0
}

// resolvedRef is a reference resolved to its record and which kind it is
type resolvedRef struct {
	id         string
	subcontrol bool
}

// matchRemote finds the remote record a file entry refers to, by ID when the
// entry has one and by key otherwise, records already matched to another
// entry are not reused
func matchRemote[R any](id, key string, byID, byKey map[string]R, consumed map[string]struct{}, idOf func(R) string) (R, bool) {
	if id != "" {
		if r, ok := byID[id]; ok {
			return r, true
		}
	}

	r, ok := byKey[key]
	if !ok {
		return r, false
	}

	if _, taken := consumed[idOf(r)]; taken {
		var zero R

		return zero, false
	}

	return r, true
}

// recordError logs a per-record failure and adds it to the run's results
func (s *applyState) recordError(ctx context.Context, err error) {
	logx.FromContext(ctx).Error().Err(err).Msg("record failed, continuing")

	s.result.Errors = append(s.result.Errors, err.Error())
}

// applyState carries the remote index and per-run caches shared across kinds
type applyState struct {
	store *Store
	// controlsByID and controlsByRefCode index the remote controls a store
	// entry can match
	controlsByID      map[string]RemoteControl
	controlsByRefCode map[string]RemoteControl
	// subcontrolsByID and subcontrolsByKey index the remote subcontrols a
	// nested entry can match, keyed by parent since refCodes repeat across controls
	subcontrolsByID  map[string]RemoteSubcontrol
	subcontrolsByKey map[string]RemoteSubcontrol
	// policiesByID indexes the remote policies a manifest entry can match
	policiesByID map[string]RemotePolicy
	// mappedTargets holds the references already mapped from each control ID
	mappedTargets map[string]controlfile.MappedControls
	// ownedMappings holds the mapping courier owns per control and framework
	ownedMappings map[string]ownedMapping
	// matched records the remote IDs a store entry has claimed so two entries
	// never resolve to the same record
	matched map[string]struct{}
	// localIDs maps store refCodes to control IDs, extended as creates resolve them
	localIDs map[string]string
	// storeRefCodes are the refCodes the inventory defines, a custom reference
	// to one of these resolves even before the control exists
	storeRefCodes map[string]struct{}
	// resolved caches framework-scoped refCode lookups
	resolved map[string]resolvedRef
	dryRun   bool
	result   *ApplyResult
}

// Apply pushes the store files through the API for the selected kinds in
// registry order. Remote state is fetched first and every record is compared
// against it, so only entries that actually differ are written and a repeated
// apply is a no-op. With DryRun the same comparison runs and the result reports
// what would change without writing anything. Nothing is ever deleted
func (c *Client) Apply(ctx context.Context, store *Store, kinds []Kind, opts ApplyOptions) (*ApplyResult, error) {
	if err := controlfile.Validate(store.Controls); err != nil {
		return nil, err
	}

	if err := controlfile.Validate(store.Policies); err != nil {
		return nil, err
	}

	controlfile.NormalizePolicies(store.Policies)

	remote, err := c.FetchState(ctx, kinds)
	if err != nil {
		return nil, err
	}

	state := &applyState{
		store: store,
		controlsByID: lo.SliceToMap(remote.Controls, func(rc RemoteControl) (string, RemoteControl) {
			return rc.ID, rc
		}),
		controlsByRefCode: lo.SliceToMap(remote.Controls, func(rc RemoteControl) (string, RemoteControl) {
			return rc.RefCode, rc
		}),
		subcontrolsByID: lo.SliceToMap(remote.Subcontrols, func(rs RemoteSubcontrol) (string, RemoteSubcontrol) {
			return rs.ID, rs
		}),
		subcontrolsByKey: lo.SliceToMap(remote.Subcontrols, func(rs RemoteSubcontrol) (string, RemoteSubcontrol) {
			return subcontrolKey(rs.ControlID, rs.RefCode), rs
		}),
		policiesByID: lo.SliceToMap(remote.Policies, func(rp RemotePolicy) (string, RemotePolicy) {
			return rp.ID, rp
		}),
		mappedTargets: mappedTargets(remote),
		ownedMappings: ownedMappings(remote),
		matched:       map[string]struct{}{},
		localIDs:      map[string]string{},
		storeRefCodes: lo.SliceToMap(store.Controls, func(doc *controlfile.Control) (string, struct{}) {
			return doc.RefCode, struct{}{}
		}),
		resolved: map[string]resolvedRef{},
		dryRun:   opts.DryRun,
		result:   &ApplyResult{},
	}

	for _, spec := range scoped(kinds) {
		if err := spec.apply(ctx, c, state); err != nil {
			return state.result, err
		}
	}

	return state.result, nil
}

// applyControlsKind upserts every control in the inventory and creates its
// mappings
func applyControlsKind(ctx context.Context, c *Client, state *applyState) error {
	for _, doc := range state.store.Controls {
		if err := c.upsertControl(ctx, state, doc); err != nil {
			state.recordError(ctx, fmt.Errorf("control %q: %w", doc.RefCode, err))

			// subcontrols are created against their parent, without it there
			// is nothing to attach them to
			continue
		}

		for _, sub := range doc.Subcontrols {
			if err := c.upsertSubcontrol(ctx, state, doc, sub); err != nil {
				state.recordError(ctx, fmt.Errorf("subcontrol %q: %w", sub.RefCode, err))
			}
		}
	}

	// mappings run after every control and subcontrol exists so references
	// between them resolve
	for _, doc := range state.store.Controls {
		for _, m := range mappableRefs(doc) {
			created, err := c.createMapping(ctx, state, m)
			state.result.CreatedMappings = append(state.result.CreatedMappings, created...)

			if err != nil {
				state.recordError(ctx, fmt.Errorf("mapping %q: %w", m.refCode, err))
			}
		}
	}

	return nil
}

// mappable is a control or subcontrol that carries mappings, the API keeps the
// two on separate edges so the from side records which it is
type mappable struct {
	id         string
	refCode    string
	targets    controlfile.MappedControls
	subcontrol bool
}

// mappableRefs lists the control and its subcontrols that declare mappings
func mappableRefs(doc *controlfile.Control) []mappable {
	var refs []mappable

	if len(doc.MappedControls) > 0 {
		refs = append(refs, mappable{id: doc.ID, refCode: doc.RefCode, targets: doc.MappedControls})
	}

	for _, sub := range doc.Subcontrols {
		if len(sub.MappedControls) > 0 {
			refs = append(refs, mappable{id: sub.ID, refCode: sub.RefCode, targets: sub.MappedControls, subcontrol: true})
		}
	}

	return refs
}

// upsertSubcontrol updates a subcontrol when its managed fields differ from
// the record in Openlane, and creates it against its parent when none matches
func (c *Client) upsertSubcontrol(ctx context.Context, state *applyState, parent *controlfile.Control, doc *controlfile.Subcontrol) error {
	remote, found := matchRemote(doc.ID, subcontrolKey(parent.ID, doc.RefCode),
		state.subcontrolsByID, state.subcontrolsByKey, state.matched,
		func(rs RemoteSubcontrol) string { return rs.ID })

	if found {
		state.matched[remote.ID] = struct{}{}
		doc.ID = remote.ID

		changed := changedControlFields(subcontrolAsControl(doc), remote.RemoteControl)
		if len(changed) == 0 {
			state.result.UnchangedControls++

			return nil
		}

		if state.dryRun {
			state.result.UpdatedControls = append(state.result.UpdatedControls, Change{Ref: doc.RefCode, Detail: changed})

			return nil
		}

		input := graphclient.UpdateSubcontrolInput{
			RefCode:     lo.EmptyableToPtr(doc.RefCode),
			Title:       lo.EmptyableToPtr(doc.Title),
			Description: lo.EmptyableToPtr(doc.Description),
			Category:    lo.EmptyableToPtr(doc.Category),
			Subcategory: lo.EmptyableToPtr(doc.Subcategory),
		}

		if len(doc.Tags) > 0 {
			input.Tags = doc.Tags
		}

		if _, err := c.typed.UpdateSubcontrol(ctx, remote.ID, input); err != nil {
			return err
		}

		state.result.UpdatedControls = append(state.result.UpdatedControls, Change{Ref: doc.RefCode, Detail: changed})

		return nil
	}

	if state.dryRun {
		state.result.CreatedControls = append(state.result.CreatedControls, Change{Ref: doc.RefCode})

		return nil
	}

	if parent.ID == "" {
		state.result.Warnings = append(state.result.Warnings,
			fmt.Sprintf("control %q has no ID, skipping subcontrol %q", parent.RefCode, doc.RefCode))

		return nil
	}

	resp, err := c.typed.CreateSubcontrol(ctx, graphclient.CreateSubcontrolInput{
		ControlID:   parent.ID,
		RefCode:     doc.RefCode,
		Title:       lo.EmptyableToPtr(doc.Title),
		Description: lo.EmptyableToPtr(doc.Description),
		Category:    lo.EmptyableToPtr(doc.Category),
		Subcategory: lo.EmptyableToPtr(doc.Subcategory),
		Tags:        doc.Tags,
	})
	if err != nil {
		if isAlreadyExists(err) {
			state.result.Warnings = append(state.result.Warnings,
				fmt.Sprintf("subcontrol %q already exists, skipped create", doc.RefCode))

			return nil
		}

		return err
	}

	doc.ID = resp.CreateSubcontrol.Subcontrol.ID
	state.result.CreatedControls = append(state.result.CreatedControls, Change{Ref: doc.RefCode})

	logx.FromContext(ctx).Debug().Str("ref_code", doc.RefCode).Str("id", doc.ID).Msg("created subcontrol")

	return nil
}

// subcontrolAsControl views a subcontrol through the shared field set so the
// same comparison covers both
func subcontrolAsControl(doc *controlfile.Subcontrol) *controlfile.Control {
	return &controlfile.Control{
		ID:          doc.ID,
		RefCode:     doc.RefCode,
		Title:       doc.Title,
		Description: doc.Description,
		Category:    doc.Category,
		Subcategory: doc.Subcategory,
		Tags:        doc.Tags,
	}
}

// subcontrolKey identifies a subcontrol by its parent and reference code,
// refCodes are only unique within a control
func subcontrolKey(controlID, refCode string) string {
	return controlID + "::" + refCode
}

// upsertControl updates a control when its managed fields differ from the
// record in Openlane, and creates it when no record matches. A control matches
// by ID when the file carries one and by refCode otherwise
func (c *Client) upsertControl(ctx context.Context, state *applyState, doc *controlfile.Control) error {
	remote, found := matchRemote(doc.ID, doc.RefCode, state.controlsByID, state.controlsByRefCode, state.matched,
		func(rc RemoteControl) string { return rc.ID })

	if found {
		state.matched[remote.ID] = struct{}{}
		state.localIDs[doc.RefCode] = remote.ID
		doc.ID = remote.ID

		changed := changedControlFields(doc, remote)
		if len(changed) == 0 {
			state.result.UnchangedControls++

			logx.FromContext(ctx).Debug().Str("ref_code", doc.RefCode).Str("id", remote.ID).Msg("control unchanged, skipping update")

			return nil
		}

		if state.dryRun {
			state.result.UpdatedControls = append(state.result.UpdatedControls, Change{Ref: doc.RefCode, Detail: changed})

			return nil
		}

		input := graphclient.UpdateControlInput{
			RefCode:     lo.EmptyableToPtr(doc.RefCode),
			Title:       lo.EmptyableToPtr(doc.Title),
			Description: lo.EmptyableToPtr(doc.Description),
			Category:    lo.EmptyableToPtr(doc.Category),
			Subcategory: lo.EmptyableToPtr(doc.Subcategory),
		}

		if len(doc.Tags) > 0 {
			input.Tags = doc.Tags
		}

		if _, err := c.typed.UpdateControl(ctx, remote.ID, input); err != nil {
			return err
		}

		state.result.UpdatedControls = append(state.result.UpdatedControls, Change{Ref: doc.RefCode, Detail: changed})

		logx.FromContext(ctx).Debug().Str("ref_code", doc.RefCode).Str("id", remote.ID).Msg("updated control")

		return nil
	}

	if state.dryRun {
		state.result.CreatedControls = append(state.result.CreatedControls, Change{Ref: doc.RefCode})

		return nil
	}

	input := graphclient.CreateControlInput{
		RefCode:     doc.RefCode,
		Title:       lo.EmptyableToPtr(doc.Title),
		Description: lo.EmptyableToPtr(doc.Description),
		Category:    lo.EmptyableToPtr(doc.Category),
		Subcategory: lo.EmptyableToPtr(doc.Subcategory),
		Tags:        doc.Tags,
		Status:      &enums.ControlStatusApproved,
	}

	resp, err := c.typed.CreateControl(ctx, input)
	if err != nil {
		if isAlreadyExists(err) {
			logx.FromContext(ctx).Debug().Str("ref_code", doc.RefCode).Msg("control already exists, skipping create")

			state.result.Warnings = append(state.result.Warnings, fmt.Sprintf("control %q already exists, skipped create", doc.RefCode))

			return nil
		}

		return err
	}

	doc.ID = resp.CreateControl.Control.ID
	state.localIDs[doc.RefCode] = doc.ID
	state.result.CreatedControls = append(state.result.CreatedControls, Change{Ref: doc.RefCode})

	logx.FromContext(ctx).Debug().Str("ref_code", doc.RefCode).Str("id", doc.ID).Msg("created control")

	return nil
}

// createMapping creates one mapped control per framework for the targets the
// control is not already mapped to, mirroring how the file groups them and the
// one record per target framework convention harmonize uses. A control whose
// mappings all exist is left alone, and the type is asserted rather than left
// to the server default so the file's meaning is explicit
func (c *Client) createMapping(ctx context.Context, state *applyState, doc mappable) ([]Change, error) {
	controlID := doc.id
	if controlID == "" && !doc.subcontrol {
		controlID = state.localIDs[doc.refCode]
	}

	// a record that does not exist yet carries no mappings, so every target is new
	missing := missingTargets(doc.targets, state.mappedTargets[controlID])
	if len(missing) == 0 {
		return nil, nil
	}

	frameworks := lo.Keys(missing)
	slices.Sort(frameworks)

	var created []Change

	for _, framework := range frameworks {
		group := controlfile.MappedControls{framework: missing[framework]}

		// resolution is read only, a dry run still reports references that will be skipped
		toIDs, resolved, err := c.resolveTargets(ctx, state, group)
		if err != nil {
			return created, err
		}

		if toIDs.empty() {
			continue
		}

		change := Change{Ref: doc.refCode, Detail: flattenTargets(resolved)}
		owned, ownsRecord := state.ownedMappings[mappingKey(controlID, framework)]

		if state.dryRun {
			// report the same way the real run will, extending an owned
			// record rather than leaving a new one behind
			if ownsRecord {
				state.result.UpdatedMappings = append(state.result.UpdatedMappings, change)
			} else {
				created = append(created, change)
			}

			continue
		}

		if controlID == "" {
			state.result.Warnings = append(state.result.Warnings, fmt.Sprintf("control %q has no ID, skipping its mappings", doc.refCode))

			return created, nil
		}

		// extend the record courier already owns for this framework rather
		// than leaving a new one behind on every apply
		if ownsRecord {
			if _, err := c.typed.UpdateMappedControl(ctx, owned.id, graphclient.UpdateMappedControlInput{
				AddToControlIDs:    toIDs.controls,
				AddToSubcontrolIDs: toIDs.subcontrols,
			}); err != nil {
				return created, err
			}

			logx.FromContext(ctx).Debug().Str("ref_code", doc.refCode).Str("framework", framework).Str("id", owned.id).Msg("extended mapping")

			state.result.UpdatedMappings = append(state.result.UpdatedMappings, change)

			continue
		}

		input := graphclient.CreateMappedControlInput{
			ToControlIDs:    toIDs.controls,
			ToSubcontrolIDs: toIDs.subcontrols,
			Source:          &enums.MappingSourceImported,
			MappingType:     &enums.MappingTypeEqual,
		}

		// the from side goes on whichever edge matches what it is
		if doc.subcontrol {
			input.FromSubcontrolIDs = []string{controlID}
		} else {
			input.FromControlIDs = []string{controlID}
		}

		if _, err := c.typed.CreateMappedControl(ctx, input); err != nil {
			if isAlreadyExists(err) {
				logx.FromContext(ctx).Debug().Str("ref_code", doc.refCode).Str("framework", framework).Msg("mapping already exists, skipping")

				continue
			}

			return created, err
		}

		logx.FromContext(ctx).Debug().Str("ref_code", doc.refCode).Str("framework", framework).Msg("created mapping")

		created = append(created, change)
	}

	return created, nil
}

// applyPoliciesKind upserts every policy from its manifest entry and document
func applyPoliciesKind(ctx context.Context, c *Client, state *applyState) error {
	for _, policy := range state.store.Policies {
		if err := c.upsertPolicy(ctx, state, policy); err != nil {
			state.recordError(ctx, fmt.Errorf("policy %q: %w", policy.Name, err))
		}
	}

	return nil
}

// upsertPolicy uploads the policy document against its ID when known,
// otherwise creates the policy and links its metadata, identity comes from
// the manifest or the document frontmatter
func (c *Client) upsertPolicy(ctx context.Context, state *applyState, policy *controlfile.Policy) error {
	_, fm, body, err := state.store.policyMarkdown(policy)
	if err != nil {
		return err
	}

	id := policy.ID
	if id == "" {
		id = fm.OpenlaneID
	}

	remote, found := state.policiesByID[id]

	// references the policy does not already carry, resolved only when there
	// is something to write
	var (
		satisfies = fm.Satisfies
		changed   []string
	)

	if found {
		satisfies = missingTargets(fm.Satisfies, linkedControls(remote))
		changed = changedPolicyFields(policy, fm, body, remote)

		if len(satisfies) == 0 && len(changed) == 0 {
			state.result.UnchangedPolicies++

			logx.FromContext(ctx).Debug().Str("name", policy.Name).Str("id", id).Msg("policy unchanged, skipping upload")

			return nil
		}
	}

	if !found && id != "" {
		logx.FromContext(ctx).Debug().Str("name", policy.Name).Str("id", id).Msg("policy id not found in Openlane, creating new policy")
	}

	if state.dryRun {
		// resolution is read only, a dry run still reports references that will be skipped
		_, resolved, err := c.resolveTargets(ctx, state, satisfies)
		if err != nil {
			return err
		}

		if !found {
			state.result.CreatedPolicies = append(state.result.CreatedPolicies, Change{Ref: policy.Name})

			return nil
		}

		state.result.UpdatedPolicies = append(state.result.UpdatedPolicies,
			Change{Ref: policy.Name, Detail: append(changed, flattenTargets(resolved)...)})

		return nil
	}

	upload, closeUpload, err := documentUpload(state.store.Dir, policy.MarkdownPath)
	if err != nil {
		return err
	}

	defer closeUpload()

	name := effectiveName(policy, fm)

	input := graphclient.UpdateInternalPolicyInput{
		Name:                   &name,
		InternalPolicyKindName: lo.EmptyableToPtr(policy.PolicyType),
		Revision:               lo.EmptyableToPtr(fm.Revision),
	}

	if fm.Status != "" {
		input.Status = enums.ToDocumentStatus(fm.Status)
	}

	if tags := effectiveTags(policy, fm); len(tags) > 0 {
		input.Tags = tags
	}

	addIDs, resolved, err := c.resolveTargets(ctx, state, satisfies)
	if err != nil {
		return err
	}

	input.AddControlIDs = addIDs.controls
	input.AddSubcontrolIDs = addIDs.subcontrols

	if found {
		if _, err := c.typed.UpdateInternalPolicyWithFile(ctx, id, *upload, input); err != nil {
			return err
		}

		state.result.UpdatedPolicies = append(state.result.UpdatedPolicies,
			Change{Ref: policy.Name, Detail: append(changed, flattenTargets(resolved)...)})

		logx.FromContext(ctx).Debug().Str("name", policy.Name).Str("id", id).Msg("updated policy")

		return nil
	}

	var ownerID *string
	if c.config.OrganizationID != "" {
		ownerID = &c.config.OrganizationID
	}

	resp, err := c.typed.CreateUploadInternalPolicy(ctx, *upload, ownerID)
	if err != nil {
		if isAlreadyExists(err) {
			state.result.Warnings = append(state.result.Warnings, fmt.Sprintf("policy %q already exists, skipped create", policy.Name))

			return nil
		}

		return err
	}

	policy.ID = resp.CreateUploadInternalPolicy.InternalPolicy.ID

	// persist the id before anything else can fail: names are not unique and
	// there is no lookup fallback, so a lost id means the next apply creates a
	// duplicate instead of updating
	if err := writeFrontmatterID(state.store, policy); err != nil {
		return err
	}

	if _, err := c.typed.UpdateInternalPolicy(ctx, policy.ID, input); err != nil {
		return err
	}

	state.result.CreatedPolicies = append(state.result.CreatedPolicies, Change{Ref: policy.Name})

	logx.FromContext(ctx).Debug().Str("name", policy.Name).Str("id", policy.ID).Msg("created policy")

	return nil
}

// writeFrontmatterID rewrites a policy document with its assigned openlane id
func writeFrontmatterID(store *Store, policy *controlfile.Policy) error {
	_, fm, body, err := store.policyMarkdown(policy)
	if err != nil {
		return err
	}

	fm.OpenlaneID = policy.ID

	doc, err := controlfile.MarshalPolicyMarkdown(fm, body)
	if err != nil {
		return err
	}

	path, err := documentPath(store.Dir, policy.MarkdownPath)
	if err != nil {
		return err
	}

	store.PolicyMarkdown[policy.MarkdownPath] = doc

	return os.WriteFile(path, doc, filePerm)
}

// resolveTargets resolves framework-grouped control references to IDs,
// custom references resolve against store controls first, framework
// references resolve via a targeted lookup, unresolvable references are
// skipped with a warning
func (c *Client) resolveTargets(ctx context.Context, state *applyState, targets controlfile.MappedControls) (targetIDs, controlfile.MappedControls, error) {
	var ids targetIDs

	resolvedRefs := controlfile.MappedControls{}

	for framework, codes := range targets {
		for _, code := range codes {
			key := mappingKey(code, framework)

			if framework == controlfile.CustomFrameworkKey {
				if id, ok := state.localIDs[code]; ok && id != "" {
					ids.controls = append(ids.controls, id)
					resolvedRefs[framework] = append(resolvedRefs[framework], code)

					continue
				}

				// a dry run has not created the control the reference points
				// at, the inventory defining it is enough to resolve
				if _, ok := state.storeRefCodes[code]; ok && state.dryRun {
					ids.controls = append(ids.controls, pendingID)
					resolvedRefs[framework] = append(resolvedRefs[framework], code)

					continue
				}
			}

			ref, ok := state.resolved[key]
			if !ok {
				var err error

				if ref, err = c.resolveRef(ctx, framework, code); err != nil {
					return ids, nil, err
				}

				state.resolved[key] = ref
			}

			if ref.id == "" {
				logx.FromContext(ctx).Debug().Str("ref_code", code).Str("framework", framework).Msg("reference not found, skipping mapping target")

				state.result.Warnings = append(state.result.Warnings, fmt.Sprintf("control %q not found in %q, skipping mapping target", code, framework))

				continue
			}

			if ref.subcontrol {
				ids.subcontrols = append(ids.subcontrols, ref.id)
			} else {
				ids.controls = append(ids.controls, ref.id)
			}

			resolvedRefs[framework] = append(resolvedRefs[framework], code)
		}
	}

	return ids, resolvedRefs, nil
}

// resolveRef resolves a reference to a control, falling back to a subcontrol,
// so the file can name either without saying which it is
func (c *Client) resolveRef(ctx context.Context, framework, code string) (resolvedRef, error) {
	id, err := c.resolveControlRefCode(ctx, framework, code)
	if err != nil {
		return resolvedRef{}, err
	}

	if id != "" {
		return resolvedRef{id: id}, nil
	}

	if id, err = c.resolveSubcontrolRefCode(ctx, framework, code); err != nil {
		return resolvedRef{}, err
	}

	return resolvedRef{id: id, subcontrol: id != ""}, nil
}

// documentUpload builds a GraphQL upload from a store document and returns the
// func that releases the open file
func documentUpload(dir, relPath string) (*graphql.Upload, func(), error) {
	path, err := documentPath(dir, relPath)
	if err != nil {
		return nil, nil, err
	}

	file, err := storage.NewUploadFile(path)
	if err != nil {
		return nil, nil, err
	}

	release := func() {
		if closer, ok := file.RawFile.(io.Closer); ok {
			closer.Close()
		}
	}

	return &graphql.Upload{
		File:        file.RawFile,
		Filename:    file.OriginalName,
		Size:        file.Size,
		ContentType: file.ContentType,
	}, release, nil
}
