package engine

import (
	"context"

	"github.com/samber/lo"

	"github.com/theopenlane/courier/pkg/controlfile"
)

// Plan is a read-only preflight of the files against Openlane: entries that
// would create records, references that do not resolve, and records that
// exist in Openlane but not in the files
type Plan struct {
	// CreateControls are the refCodes of controls that would be created
	CreateControls []string `json:"createControls,omitempty"`
	// CreatePolicies are the names of policies that would be created
	CreatePolicies []string `json:"createPolicies,omitempty"`
	// UnresolvedRefs are control references that resolve to nothing and
	// would be skipped on apply
	UnresolvedRefs []string `json:"unresolvedRefs,omitempty"`
	// DriftControls exist in Openlane but not in the files
	DriftControls []RemoteControl `json:"driftControls,omitempty"`
	// DriftMappings are mapped references present in Openlane but absent from the files
	DriftMappings controlfile.MappedControls `json:"driftMappings,omitempty"`
	// DriftPolicies exist in Openlane but not in the files
	DriftPolicies []RemotePolicy `json:"driftPolicies,omitempty"`
}

// HasFindings reports whether the preflight found anything worth review
func (p *Plan) HasFindings() bool {
	return len(p.CreateControls)+len(p.CreatePolicies)+len(p.UnresolvedRefs)+
		len(p.DriftControls)+len(p.DriftMappings)+len(p.DriftPolicies) > 0
}

// Plan fetches remote state for the selected kinds and preflights the files
// against it
func (c *Client) Plan(ctx context.Context, store *Store, kinds []Kind) (*Plan, error) {
	controlfile.NormalizePolicies(store.Policies)

	if err := controlfile.Validate(store.Controls); err != nil {
		return nil, err
	}

	if err := controlfile.Validate(store.Policies); err != nil {
		return nil, err
	}

	state, err := c.FetchState(ctx, kinds)
	if err != nil {
		return nil, err
	}

	plan := &Plan{}
	resolved := map[string]string{}

	for _, spec := range scoped(kinds) {
		if err := spec.plan(ctx, c, plan, store, state, resolved); err != nil {
			return nil, err
		}
	}

	return plan, nil
}

// planControls reports controls that would be created, unresolved mapping
// references, and drift
func planControls(ctx context.Context, c *Client, plan *Plan, store *Store, state *RemoteState, resolved map[string]string) error {
	byID := lo.SliceToMap(state.Controls, func(rc RemoteControl) (string, RemoteControl) { return rc.ID, rc })
	byRefCode := lo.SliceToMap(state.Controls, func(rc RemoteControl) (string, RemoteControl) {
		return rc.RefCode, rc
	})

	matched := map[string]struct{}{}
	fileRefCodes := map[string]struct{}{}

	for _, doc := range store.Controls {
		fileRefCodes[doc.RefCode] = struct{}{}
	}

	for _, doc := range store.Controls {
		remote, ok := matchRemote(doc.ID, doc.RefCode, byID, byRefCode, matched, func(rc RemoteControl) string { return rc.ID })
		if ok {
			matched[remote.ID] = struct{}{}
		} else {
			plan.CreateControls = append(plan.CreateControls, doc.RefCode)
		}

		unresolved, err := c.unresolvedRefs(ctx, doc.MappedControls, fileRefCodes, resolved)
		if err != nil {
			return err
		}

		for _, ref := range unresolved {
			plan.UnresolvedRefs = append(plan.UnresolvedRefs, ref+" (control "+doc.RefCode+")")
		}
	}

	for _, rc := range state.Controls {
		if _, ok := matched[rc.ID]; !ok {
			plan.DriftControls = append(plan.DriftControls, rc)
		}
	}

	plan.DriftMappings = missingMappedRefs(store, state)

	return nil
}

// planPolicies reports policies that would be created, unresolved satisfies
// references, and drift
func planPolicies(ctx context.Context, c *Client, plan *Plan, store *Store, state *RemoteState, resolved map[string]string) error {
	byID := lo.SliceToMap(state.Policies, func(rp RemotePolicy) (string, RemotePolicy) { return rp.ID, rp })

	fileRefCodes := map[string]struct{}{}
	for _, doc := range store.Controls {
		fileRefCodes[doc.RefCode] = struct{}{}
	}

	matched := map[string]struct{}{}

	for _, policy := range store.Policies {
		_, fm, _, err := store.policyMarkdown(policy)
		if err != nil {
			return err
		}

		id := policy.ID
		if id == "" {
			id = fm.OpenlaneID
		}

		if _, ok := byID[id]; ok {
			matched[id] = struct{}{}
		} else {
			plan.CreatePolicies = append(plan.CreatePolicies, policy.Name)
		}

		unresolved, err := c.unresolvedRefs(ctx, fm.Satisfies, fileRefCodes, resolved)
		if err != nil {
			return err
		}

		for _, ref := range unresolved {
			plan.UnresolvedRefs = append(plan.UnresolvedRefs, ref+" (policy "+policy.Name+")")
		}
	}

	for _, rp := range state.Policies {
		if _, ok := matched[rp.ID]; !ok {
			plan.DriftPolicies = append(plan.DriftPolicies, rp)
		}
	}

	return nil
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

// unresolvedRefs returns the references in a framework-grouped map that
// resolve to nothing, custom references check the files first
func (c *Client) unresolvedRefs(ctx context.Context, refs controlfile.MappedControls, fileRefCodes map[string]struct{}, resolved map[string]string) ([]string, error) {
	var unresolved []string

	for framework, codes := range refs {
		for _, code := range codes {
			if framework == controlfile.CustomFrameworkKey {
				if _, ok := fileRefCodes[code]; ok {
					continue
				}
			}

			key := framework + "::" + code

			id, ok := resolved[key]
			if !ok {
				var err error

				id, err = c.resolveControlRefCode(ctx, framework, code)
				if err != nil {
					return nil, err
				}

				resolved[key] = id
			}

			if id == "" {
				unresolved = append(unresolved, framework+"::"+code)
			}
		}
	}

	return unresolved, nil
}

// missingMappedRefs returns the mapped references present in Openlane but
// absent from the files, grouped by framework
func missingMappedRefs(store *Store, state *RemoteState) controlfile.MappedControls {
	inFiles := map[string]struct{}{}

	for _, doc := range store.Controls {
		for framework, codes := range doc.MappedControls {
			for _, code := range codes {
				inFiles[framework+"::"+code] = struct{}{}
			}
		}
	}

	missing := controlfile.MappedControls{}

	for _, mapping := range state.Mappings {
		for _, to := range mapping.To {
			framework := to.Framework
			if framework == "" {
				framework = controlfile.CustomFrameworkKey
			}

			if _, ok := inFiles[framework+"::"+to.RefCode]; !ok {
				missing[framework] = lo.Uniq(append(missing[framework], to.RefCode))
			}
		}
	}

	if len(missing) == 0 {
		return nil
	}

	return missing
}
