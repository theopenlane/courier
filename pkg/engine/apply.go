package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/99designs/gqlgen/graphql"
	"github.com/samber/lo"

	"github.com/theopenlane/core/common/enums"
	"github.com/theopenlane/core/pkg/logx"
	"github.com/theopenlane/core/pkg/objects/storage"
	"github.com/theopenlane/go-client/graphclient"

	"github.com/theopenlane/courier/pkg/controlfile"
)

// ApplyResult summarizes what an apply changed in the API
type ApplyResult struct {
	// CreatedControls are the refCodes of controls created, in order
	CreatedControls []string `json:"createdControls,omitempty"`
	// UpdatedControls are the refCodes of controls updated, in order
	UpdatedControls []string `json:"updatedControls,omitempty"`
	// CreatedMappings counts mapped controls created
	CreatedMappings int `json:"createdMappings"`
	// CreatedPolicies are the names of policies created, in order
	CreatedPolicies []string `json:"createdPolicies,omitempty"`
	// UpdatedPolicies are the names of policies updated, in order
	UpdatedPolicies []string `json:"updatedPolicies,omitempty"`
	// Warnings are non-fatal issues such as mapped control refCodes that
	// could not be resolved and were skipped
	Warnings []string `json:"warnings,omitempty"`
	// Errors are per-record failures, the run continues past them so one
	// rejected record does not abort the batch
	Errors []string `json:"errors,omitempty"`
}

// recordError logs a per-record failure and adds it to the run's results
func (s *applyState) recordError(ctx context.Context, err error) {
	logx.FromContext(ctx).Error().Err(err).Msg("record failed, continuing")

	s.result.Errors = append(s.result.Errors, err.Error())
}

// applyState carries the per-run caches shared across kinds
type applyState struct {
	store *Store
	// localIDs maps store refCodes to control IDs, extended as creates and
	// lookups resolve them
	localIDs map[string]string
	// resolved caches framework-scoped refCode lookups
	resolved map[string]string
	result   *ApplyResult
}

// Apply pushes the store files through the API for the selected
// kinds in registry order. Records with an ID update directly, records
// without one are looked up individually and created when absent, nothing is
// ever deleted and no state is fetched up front
func (c *Client) Apply(ctx context.Context, store *Store, kinds []Kind) (*ApplyResult, error) {
	controlfile.NormalizePolicies(store.Policies)

	if err := controlfile.Validate(store.Controls); err != nil {
		return nil, err
	}

	if err := controlfile.Validate(store.Policies); err != nil {
		return nil, err
	}

	state := &applyState{
		store:    store,
		localIDs: map[string]string{},
		resolved: map[string]string{},
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
// mappings, the server rejects duplicates so no state is fetched
func applyControlsKind(ctx context.Context, c *Client, state *applyState) error {
	for _, doc := range state.store.Controls {
		if err := c.upsertControl(ctx, state, doc); err != nil {
			state.recordError(ctx, fmt.Errorf("control %q: %w", doc.RefCode, err))
		}
	}

	for _, doc := range state.store.Controls {
		if len(doc.MappedControls) == 0 {
			continue
		}

		created, err := c.createMapping(ctx, state, doc)
		if err != nil {
			state.recordError(ctx, fmt.Errorf("mapping control %q: %w", doc.RefCode, err))

			continue
		}

		if created {
			state.result.CreatedMappings++
		}
	}

	return nil
}

// upsertControl updates a control by ID when known, otherwise checks for an
// existing control by refCode and updates the match or creates the control
func (c *Client) upsertControl(ctx context.Context, state *applyState, doc *controlfile.Control) error {
	id := doc.ID
	if id == "" {
		// check for existing control first
		existing, err := c.typed.GetControls(ctx, nil, nil, nil, nil, &graphclient.ControlWhereInput{
			RefCode: &doc.RefCode,
		}, nil)
		if err == nil && len(existing.Controls.Edges) > 0 {
			id = existing.Controls.Edges[0].GetNode().ID

			logx.FromContext(ctx).Debug().Str("ref_code", doc.RefCode).Str("id", id).Msg("control already exists, updating")
		}
	}

	if id != "" {
		state.localIDs[doc.RefCode] = id

		input := graphclient.UpdateControlInput{
			Title:       lo.EmptyableToPtr(doc.Title),
			Description: lo.EmptyableToPtr(doc.Description),
			Category:    lo.EmptyableToPtr(doc.Category),
			Subcategory: lo.EmptyableToPtr(doc.Subcategory),
		}

		if len(doc.Tags) > 0 {
			input.Tags = doc.Tags
		}

		if _, err := c.typed.UpdateControl(ctx, id, input); err != nil {
			return err
		}

		state.result.UpdatedControls = append(state.result.UpdatedControls, doc.RefCode)

		logx.FromContext(ctx).Debug().Str("ref_code", doc.RefCode).Str("id", id).Msg("updated control")

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
	state.result.CreatedControls = append(state.result.CreatedControls, doc.RefCode)

	logx.FromContext(ctx).Debug().Str("ref_code", doc.RefCode).Str("id", doc.ID).Msg("created control")

	return nil
}

// createMapping creates one mapped control from the control to every
// resolvable target, the server rejects duplicate mappings
func (c *Client) createMapping(ctx context.Context, state *applyState, doc *controlfile.Control) (bool, error) {
	controlID := doc.ID
	if controlID == "" {
		controlID = state.localIDs[doc.RefCode]
	}

	if controlID == "" {
		state.result.Warnings = append(state.result.Warnings, fmt.Sprintf("control %q has no ID, skipping its mappings", doc.RefCode))

		return false, nil
	}

	toIDs, err := c.resolveTargets(ctx, state, doc.MappedControls)
	if err != nil {
		return false, err
	}

	if len(toIDs) == 0 {
		return false, nil
	}

	input := graphclient.CreateMappedControlInput{
		FromControlIDs: []string{controlID},
		ToControlIDs:   toIDs,
		Source:         &enums.MappingSourceImported,
	}

	if _, err := c.typed.CreateMappedControl(ctx, input); err != nil {
		if isAlreadyExists(err) {
			logx.FromContext(ctx).Debug().Str("ref_code", doc.RefCode).Msg("mapping already exists, skipping")

			return false, nil
		}

		return false, err
	}

	logx.FromContext(ctx).Debug().Str("ref_code", doc.RefCode).Msg("created mapping")

	return true, nil
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
	_, fm, _, err := state.store.policyMarkdown(policy)
	if err != nil {
		return err
	}

	id := policy.ID
	if id == "" {
		id = fm.OpenlaneID
	}

	if id == "" {
		logx.FromContext(ctx).Debug().Str("name", policy.Name).Msg("no id found for policy, creating new policy")
	}

	upload, err := documentUpload(state.store.Dir, policy.MarkdownPath)
	if err != nil {
		return err
	}

	input := graphclient.UpdateInternalPolicyInput{
		Name:                   &policy.Name,
		InternalPolicyKindName: lo.EmptyableToPtr(policy.PolicyType),
		Revision:               lo.EmptyableToPtr(fm.Revision),
	}

	if fm.Status != "" {
		input.Status = enums.ToDocumentStatus(fm.Status)
	}

	if len(policy.Tags) > 0 {
		input.Tags = policy.Tags
	}

	if input.AddControlIDs, err = c.resolveTargets(ctx, state, fm.Satisfies); err != nil {
		return err
	}

	if id != "" {
		if _, err := c.typed.UpdateInternalPolicyWithFile(ctx, id, *upload, input); err != nil {
			return err
		}

		state.result.UpdatedPolicies = append(state.result.UpdatedPolicies, policy.Name)

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

	if _, err := c.typed.UpdateInternalPolicy(ctx, policy.ID, input); err != nil {
		return err
	}

	state.result.CreatedPolicies = append(state.result.CreatedPolicies, policy.Name)

	logx.FromContext(ctx).Debug().Str("name", policy.Name).Str("id", policy.ID).Msg("created policy, updating document with frontmatter")

	// write the assigned id back into the document frontmatter so the next
	// apply updates by id instead of creating a duplicate, policy names are
	// not unique so there is no lookup fallback
	return writeFrontmatterID(state.store, policy)
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

	path := filepath.Join(store.Dir, filepath.FromSlash(policy.MarkdownPath))

	return os.WriteFile(path, doc, filePerm)
}

// resolveTargets resolves framework-grouped control references to IDs,
// custom references resolve against store controls first, framework
// references resolve via a targeted lookup, unresolvable references are
// skipped with a warning
func (c *Client) resolveTargets(ctx context.Context, state *applyState, targets controlfile.MappedControls) ([]string, error) {
	var ids []string

	for framework, codes := range targets {
		for _, code := range codes {
			key := framework + "::" + code

			if framework == controlfile.CustomFrameworkKey {
				if id, ok := state.localIDs[code]; ok && id != "" {
					ids = append(ids, id)

					continue
				}
			}

			id, ok := state.resolved[key]
			if !ok {
				var err error

				id, err = c.resolveControlRefCode(ctx, framework, code)
				if err != nil {
					return nil, err
				}

				state.resolved[key] = id
			}

			if id == "" {
				logx.FromContext(ctx).Debug().Str("ref_code", code).Str("framework", framework).Msg("control not found, skipping mapping target")

				state.result.Warnings = append(state.result.Warnings, fmt.Sprintf("control %q not found in %q, skipping mapping target", code, framework))

				continue
			}

			ids = append(ids, id)
		}
	}

	return ids, nil
}

// documentUpload builds a GraphQL upload from a store document using the
// same file construction as the openlane CLI
func documentUpload(dir, relPath string) (*graphql.Upload, error) {
	file, err := storage.NewUploadFile(filepath.Join(dir, filepath.FromSlash(relPath)))
	if err != nil {
		return nil, err
	}

	return &graphql.Upload{
		File:        file.RawFile,
		Filename:    file.OriginalName,
		Size:        file.Size,
		ContentType: file.ContentType,
	}, nil
}
