package engine

import (
	"html"

	"github.com/samber/lo"

	"github.com/theopenlane/courier/pkg/controlfile"
)

// BuildControls converts remote state into the control inventory verbatim,
// the mappedControls list of a control is the refCode set on the to side of
// every mapping the control appears on the from side of, mappings are
// directional with the organization control as the from side
func BuildControls(state *RemoteState) []*controlfile.Control {
	targetsByControl := map[string][]string{}

	for _, mapping := range state.Mappings {
		targets := lo.Map(mapping.To, func(r RemoteRef, _ int) string { return r.RefCode })

		for _, from := range mapping.From {
			targetsByControl[from.ID] = lo.Uniq(append(targetsByControl[from.ID], targets...))
		}
	}

	return lo.Map(state.Controls, func(rc RemoteControl, _ int) *controlfile.Control {
		return &controlfile.Control{
			ID:             rc.ID,
			RefCode:        rc.RefCode,
			Title:          rc.Title,
			Description:    rc.Description,
			Category:       rc.Category,
			Subcategory:    rc.Subcategory,
			Tags:           rc.Tags,
			MappedControls: targetsByControl[rc.ID],
		}
	})
}

// BuildPolicies converts remote policies into the manifest plus one markdown
// document per policy, keyed by workspace-relative path
func BuildPolicies(state *RemoteState) ([]*controlfile.Policy, map[string][]byte, error) {
	policies := make([]*controlfile.Policy, 0, len(state.Policies))
	markdown := map[string][]byte{}

	for _, rp := range state.Policies {
		policy := &controlfile.Policy{
			ID:             rp.ID,
			Name:           rp.Name,
			PolicyType:     lo.FromPtr(rp.KindName),
			MarkdownPath:   controlfile.PolicyMarkdownPath(rp.Name),
			Tags:           rp.Tags,
			MappedControls: lo.Map(rp.Controls, func(r RemoteRef, _ int) string { return r.RefCode }),
		}

		// stored details are entity-escaped by the server on import, unescape
		// so the exported markdown stays human readable, plan comparison
		// re-sanitizes both sides so this stays a clean round trip
		doc, err := controlfile.MarshalPolicyMarkdown(controlfile.Frontmatter{
			Title: rp.Name,
			Tags:  rp.Tags,
		}, html.UnescapeString(lo.FromPtr(rp.Details)))
		if err != nil {
			return nil, nil, err
		}

		markdown[policy.MarkdownPath] = doc
		policies = append(policies, policy)
	}

	return policies, markdown, nil
}
