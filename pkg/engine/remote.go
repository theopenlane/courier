package engine

import (
	"context"

	"github.com/samber/lo"

	"github.com/theopenlane/core/common/enums"
	"github.com/theopenlane/go-client/graphclient"

	"github.com/theopenlane/courier/pkg/controlfile"
)

// defaultPageSize is the number of records fetched per page
const defaultPageSize = int64(100)

// RemoteControl is the authorable view of a control as it exists in the API
type RemoteControl struct {
	// ID is the Openlane ULID of the control
	ID string `json:"id"`
	// RefCode is the unique reference code of the control
	RefCode string `json:"refCode"`
	// Title is the human readable title of the control
	Title string `json:"title"`
	// Description describes what the control is supposed to accomplish
	Description string `json:"description"`
	// Category is the category of the control
	Category string `json:"category"`
	// Subcategory is the subcategory of the control
	Subcategory string `json:"subcategory"`
	// ReferenceFramework is the framework short name when the control derives from a standard
	ReferenceFramework string `json:"referenceFramework"`
	// Tags associated with the control
	Tags []string `json:"tags"`
}

// RemoteRef identifies a control or subcontrol participating in a mapping or
// policy edge
type RemoteRef struct {
	// ID is the Openlane ULID of the referenced record
	ID string `json:"id"`
	// RefCode is the reference code of the referenced record
	RefCode string `json:"refCode"`
	// Framework is the short name of the framework the record derives from,
	// empty for organization custom controls
	Framework string `json:"framework"`
	// Subcontrol reports whether the reference is a subcontrol rather than a
	// control, the two live in separate edge sets on a mapping
	Subcontrol bool `json:"subcontrol,omitempty"`
}

// RemoteMapping is a mapped-control record as it exists in the API, the
// participant edges derive mappedControls lists and the source identifies the
// records courier owns and may edit in place
type RemoteMapping struct {
	// ID is the Openlane ULID of the mapped control record
	ID string `json:"id"`
	// Source is how the mapping was created, courier owns the imported ones
	Source string `json:"source"`
	// From are the controls and subcontrols on the from side of the mapping
	From []RemoteRef `json:"from"`
	// To are the controls and subcontrols on the to side of the mapping
	To []RemoteRef `json:"to"`
}

// RemotePolicy is an internal policy as it exists in the API
type RemotePolicy struct {
	// ID is the Openlane ULID of the policy
	ID string `json:"id"`
	// Name is the unique name of the policy
	Name string `json:"name"`
	// KindName is the policy kind, e.g. Security, Operational
	KindName *string `json:"internalPolicyKindName"`
	// Status is the document status, e.g. PUBLISHED, DRAFT
	Status string `json:"status"`
	// Revision is the document revision, e.g. v1.0.0
	Revision string `json:"revision"`
	// Details is the stored policy body
	Details *string `json:"details"`
	// Tags associated with the policy
	Tags []string `json:"tags"`
	// Controls are the controls linked to the policy
	Controls []RemoteRef `json:"controls"`
}

// RemoteSubcontrol is a subcontrol as it exists in the API, it carries the
// same authorable fields as a control plus the control it belongs to
type RemoteSubcontrol struct {
	// RemoteControl holds the fields a subcontrol shares with a control
	RemoteControl
	// ControlID is the Openlane ULID of the parent control
	ControlID string `json:"controlID"`
}

// RemoteState is the full set of organization controls, mappings, and
// policies pulled from the API
type RemoteState struct {
	// Controls are the organization-owned controls not derived from a framework
	Controls []RemoteControl
	// Subcontrols are the organization-owned subcontrols of those controls
	Subcontrols []RemoteSubcontrol
	// Mappings are the organization-owned mapped-control records
	Mappings []RemoteMapping
	// Policies are the organization-owned internal policies
	Policies []RemotePolicy
}

// pager is the page-info shape shared by generated connection types
type pager interface {
	GetHasNextPage() bool
	GetEndCursor() *string
}

// paginate invokes fetch with the next cursor until the last page, fetch
// collects nodes itself and returns the page info of the fetched page
func paginate[P pager](fetch func(after *string) (P, error)) error {
	var after *string

	for {
		page, err := fetch(after)
		if err != nil {
			return err
		}

		if !page.GetHasNextPage() {
			return nil
		}

		next := page.GetEndCursor()
		if next == nil || (after != nil && *next == *after) {
			return ErrPaginationStalled
		}

		after = next
	}
}

// organizationControlsWhere selects org-owned controls that are user
// manageable, framework-derived and system-owned controls are excluded.
// Framework clones reject writes to the fields courier manages and the global
// catalog rejects writes outright, so both are filtered here rather than
// discovered as a per-record failure
func organizationControlsWhere() *graphclient.ControlWhereInput {
	return &graphclient.ControlWhereInput{
		SourceNotIn:   []enums.ControlSource{enums.ControlSourceFramework},
		SystemOwned:   new(false),
		OwnerIDNotNil: new(true),
	}
}

// FetchState pulls the remote state for the selected kinds from the API
func (c *Client) FetchState(ctx context.Context, kinds []Kind) (*RemoteState, error) {
	state := &RemoteState{}

	for _, spec := range scoped(kinds) {
		if err := spec.fetch(ctx, c, state); err != nil {
			return nil, err
		}
	}

	return state, nil
}

// fetchControlsKind retrieves organization controls, their subcontrols, and
// the mappings both participate in
func fetchControlsKind(ctx context.Context, c *Client, state *RemoteState) error {
	controls, err := c.fetchControls(ctx, organizationControlsWhere())
	if err != nil {
		return err
	}

	subcontrols, err := c.fetchSubcontrols(ctx)
	if err != nil {
		return err
	}

	mappings, err := c.fetchMappings(ctx)
	if err != nil {
		return err
	}

	state.Controls = controls
	state.Subcontrols = subcontrols
	state.Mappings = mappings

	return nil
}

// fetchSubcontrols pages through the organization-owned subcontrols that are
// user manageable, scoped the same way controls are
func (c *Client) fetchSubcontrols(ctx context.Context) ([]RemoteSubcontrol, error) {
	var subcontrols []RemoteSubcontrol

	where := &graphclient.SubcontrolWhereInput{
		SourceNotIn:   []enums.ControlSource{enums.ControlSourceFramework},
		SystemOwned:   new(false),
		OwnerIDNotNil: new(true),
	}

	err := paginate(func(after *string) (*graphclient.GetSubcontrols_Subcontrols_PageInfo, error) {
		resp, err := c.typed.GetSubcontrols(ctx, new(defaultPageSize), nil, after, nil, where, nil)
		if err != nil {
			return nil, err
		}

		for _, edge := range resp.Subcontrols.Edges {
			node := edge.GetNode()
			subcontrols = append(subcontrols, RemoteSubcontrol{
				RemoteControl: RemoteControl{
					ID:                 node.ID,
					RefCode:            node.RefCode,
					Title:              lo.FromPtr(node.Title),
					Description:        lo.FromPtr(node.Description),
					Category:           lo.FromPtr(node.Category),
					Subcategory:        lo.FromPtr(node.Subcategory),
					ReferenceFramework: lo.FromPtr(node.ReferenceFramework),
					Tags:               node.Tags,
				},
				ControlID: node.ControlID,
			})
		}

		return &resp.Subcontrols.PageInfo, nil
	})

	return subcontrols, err
}

// fetchPoliciesKind retrieves organization policies and their control references
func fetchPoliciesKind(ctx context.Context, c *Client, state *RemoteState) error {
	policies, err := c.fetchPolicies(ctx)
	if err != nil {
		return err
	}

	state.Policies = policies

	return nil
}

// fetchControls pages through the controls query with the given filter
func (c *Client) fetchControls(ctx context.Context, where *graphclient.ControlWhereInput) ([]RemoteControl, error) {
	var controls []RemoteControl

	err := paginate(func(after *string) (*graphclient.GetControls_Controls_PageInfo, error) {
		resp, err := c.typed.GetControls(ctx, new(defaultPageSize), nil, after, nil, where, nil)
		if err != nil {
			return nil, err
		}

		for _, edge := range resp.Controls.Edges {
			node := edge.GetNode()
			controls = append(controls, RemoteControl{
				ID:                 node.ID,
				RefCode:            node.RefCode,
				Title:              lo.FromPtr(node.Title),
				Description:        lo.FromPtr(node.Description),
				Category:           lo.FromPtr(node.Category),
				Subcategory:        lo.FromPtr(node.Subcategory),
				ReferenceFramework: lo.FromPtr(node.ReferenceFramework),
				Tags:               node.Tags,
			})
		}

		return &resp.Controls.PageInfo, nil
	})

	return controls, err
}

// fetchMappings pages through the organization-owned mapped controls, the
// participant edges of each mapping are paginated separately via the per-ID
// queries so large mappings are never truncated
func (c *Client) fetchMappings(ctx context.Context) ([]RemoteMapping, error) {
	sources := map[string]string{}

	var ids []string

	where := &graphclient.MappedControlWhereInput{SystemOwned: new(false)}

	err := paginate(func(after *string) (*graphclient.GetMappedControls_MappedControls_PageInfo, error) {
		resp, err := c.typed.GetMappedControls(ctx, new(defaultPageSize), nil, after, nil, where, nil)
		if err != nil {
			return nil, err
		}

		for _, edge := range resp.MappedControls.Edges {
			node := edge.GetNode()
			ids = append(ids, node.ID)

			if node.Source != nil {
				sources[node.ID] = node.Source.String()
			}
		}

		return &resp.MappedControls.PageInfo, nil
	})
	if err != nil {
		return nil, err
	}

	mappings := make([]RemoteMapping, 0, len(ids))

	for _, id := range ids {
		from, err := c.mappedFromControls(ctx, id)
		if err != nil {
			return nil, err
		}

		fromSubs, err := c.mappedFromSubcontrols(ctx, id)
		if err != nil {
			return nil, err
		}

		to, err := c.mappedToControls(ctx, id)
		if err != nil {
			return nil, err
		}

		toSubs, err := c.mappedToSubcontrols(ctx, id)
		if err != nil {
			return nil, err
		}

		mappings = append(mappings, RemoteMapping{
			ID:     id,
			Source: sources[id],
			From:   append(from, fromSubs...),
			To:     append(to, toSubs...),
		})
	}

	return mappings, nil
}

// mappedFromSubcontrols pages through the from-side subcontrols of a mapping
func (c *Client) mappedFromSubcontrols(ctx context.Context, mappingID string) ([]RemoteRef, error) {
	var refs []RemoteRef

	err := paginate(func(after *string) (*graphclient.GetMappedAllFromSubcontrolsForID_MappedControl_FromSubcontrols_PageInfo, error) {
		resp, err := c.typed.GetMappedAllFromSubcontrolsForID(ctx, mappingID, new(defaultPageSize), nil, after, nil, nil)
		if err != nil {
			return nil, err
		}

		for _, edge := range resp.MappedControl.FromSubcontrols.Edges {
			node := edge.GetNode()
			refs = append(refs, RemoteRef{
				ID:         node.ID,
				RefCode:    node.RefCode,
				Framework:  lo.FromPtr(node.ReferenceFramework),
				Subcontrol: true,
			})
		}

		return &resp.MappedControl.FromSubcontrols.PageInfo, nil
	})

	return refs, err
}

// mappedToSubcontrols pages through the to-side subcontrols of a mapping
func (c *Client) mappedToSubcontrols(ctx context.Context, mappingID string) ([]RemoteRef, error) {
	var refs []RemoteRef

	err := paginate(func(after *string) (*graphclient.GetMappedAllToSubcontrolsForID_MappedControl_ToSubcontrols_PageInfo, error) {
		resp, err := c.typed.GetMappedAllToSubcontrolsForID(ctx, mappingID, new(defaultPageSize), nil, after, nil, nil)
		if err != nil {
			return nil, err
		}

		for _, edge := range resp.MappedControl.ToSubcontrols.Edges {
			node := edge.GetNode()
			refs = append(refs, RemoteRef{
				ID:         node.ID,
				RefCode:    node.RefCode,
				Framework:  lo.FromPtr(node.ReferenceFramework),
				Subcontrol: true,
			})
		}

		return &resp.MappedControl.ToSubcontrols.PageInfo, nil
	})

	return refs, err
}

// mappedFromControls pages through the from-side controls of a mapping
func (c *Client) mappedFromControls(ctx context.Context, mappingID string) ([]RemoteRef, error) {
	var refs []RemoteRef

	err := paginate(func(after *string) (*graphclient.GetMappedAllFromControlsForID_MappedControl_FromControls_PageInfo, error) {
		resp, err := c.typed.GetMappedAllFromControlsForID(ctx, mappingID, new(defaultPageSize), nil, after, nil, nil)
		if err != nil {
			return nil, err
		}

		for _, edge := range resp.MappedControl.FromControls.Edges {
			node := edge.GetNode()
			refs = append(refs, RemoteRef{ID: node.ID, RefCode: node.RefCode, Framework: lo.FromPtr(node.ReferenceFramework)})
		}

		return &resp.MappedControl.FromControls.PageInfo, nil
	})

	return refs, err
}

// mappedToControls pages through the to-side controls of a mapping
func (c *Client) mappedToControls(ctx context.Context, mappingID string) ([]RemoteRef, error) {
	var refs []RemoteRef

	err := paginate(func(after *string) (*graphclient.GetMappedAllToControlsForID_MappedControl_ToControls_PageInfo, error) {
		resp, err := c.typed.GetMappedAllToControlsForID(ctx, mappingID, new(defaultPageSize), nil, after, nil, nil)
		if err != nil {
			return nil, err
		}

		for _, edge := range resp.MappedControl.ToControls.Edges {
			node := edge.GetNode()
			refs = append(refs, RemoteRef{ID: node.ID, RefCode: node.RefCode, Framework: lo.FromPtr(node.ReferenceFramework)})
		}

		return &resp.MappedControl.ToControls.PageInfo, nil
	})

	return refs, err
}

// fetchPolicies pages through the organization-owned internal policies and
// resolves each policy's linked controls
func (c *Client) fetchPolicies(ctx context.Context) ([]RemotePolicy, error) {
	var policies []RemotePolicy

	where := &graphclient.InternalPolicyWhereInput{SystemOwned: new(false)}

	err := paginate(func(after *string) (*graphclient.GetInternalPolicies_InternalPolicies_PageInfo, error) {
		resp, err := c.typed.GetInternalPolicies(ctx, new(defaultPageSize), nil, after, nil, where, nil)
		if err != nil {
			return nil, err
		}

		for _, edge := range resp.InternalPolicies.Edges {
			node := edge.GetNode()
			status := ""
			if node.Status != nil {
				status = node.Status.String()
			}

			policies = append(policies, RemotePolicy{
				ID:       node.ID,
				Name:     node.Name,
				KindName: node.InternalPolicyKindName,
				Status:   status,
				Revision: lo.FromPtr(node.Revision),
				Details:  node.Details,
				Tags:     node.Tags,
			})
		}

		return &resp.InternalPolicies.PageInfo, nil
	})
	if err != nil {
		return nil, err
	}

	for i := range policies {
		if policies[i].Controls, err = c.policyControls(ctx, policies[i].ID); err != nil {
			return nil, err
		}
	}

	return policies, nil
}

// policyControls returns the controls linked to a policy
func (c *Client) policyControls(ctx context.Context, policyID string) ([]RemoteRef, error) {
	controls, err := c.fetchControls(ctx, &graphclient.ControlWhereInput{
		HasInternalPoliciesWith: []*graphclient.InternalPolicyWhereInput{{ID: &policyID}},
	})
	if err != nil {
		return nil, err
	}

	return lo.Map(controls, func(rc RemoteControl, _ int) RemoteRef {
		return RemoteRef{ID: rc.ID, RefCode: rc.RefCode, Framework: rc.ReferenceFramework}
	}), nil
}

// resolveSubcontrolRefCode resolves a reference to a subcontrol ID the same
// way resolveControlRefCode resolves controls, a reference that names neither
// resolves to nothing and is skipped by the caller
func (c *Client) resolveSubcontrolRefCode(ctx context.Context, framework, refCode string) (string, error) {
	where := &graphclient.SubcontrolWhereInput{
		RefCodeEqualFold: &refCode,
		OwnerIDNotNil:    new(true),
		SystemOwned:      new(false),
	}

	if framework == controlfile.CustomFrameworkKey {
		where.ReferenceFrameworkIsNil = new(true)
	} else {
		where.ReferenceFrameworkEqualFold = &framework
	}

	var matches []string

	err := paginate(func(after *string) (*graphclient.GetSubcontrols_Subcontrols_PageInfo, error) {
		resp, err := c.typed.GetSubcontrols(ctx, new(defaultPageSize), nil, after, nil, where, nil)
		if err != nil {
			return nil, err
		}

		for _, edge := range resp.Subcontrols.Edges {
			matches = append(matches, edge.GetNode().ID)
		}

		return &resp.Subcontrols.PageInfo, nil
	})
	if err != nil {
		return "", err
	}

	switch len(matches) {
	case 0:
		return "", nil
	case 1:
		return matches[0], nil
	default:
		return "", fmtErr(ErrMultipleControlsFound, refCode)
	}
}

// resolveControlRefCode resolves a mapped control reference to a control ID
// via a case-insensitive refCode match on org-owned controls, scoped to the
// given framework short name or to controls without a framework when the
// framework is the custom key. A missing control returns an empty ID so
// callers can skip it, multiple matches are an error
func (c *Client) resolveControlRefCode(ctx context.Context, framework, refCode string) (string, error) {
	where := &graphclient.ControlWhereInput{
		RefCodeEqualFold: &refCode,
		OwnerIDNotNil:    new(true),
	}

	if framework == controlfile.CustomFrameworkKey {
		where.ReferenceFrameworkIsNil = new(true)
	} else {
		where.ReferenceFrameworkEqualFold = &framework
	}

	matches, err := c.fetchControls(ctx, where)
	if err != nil {
		return "", err
	}

	switch len(matches) {
	case 0:
		return "", nil
	case 1:
		return matches[0].ID, nil
	default:
		return "", fmtErr(ErrMultipleControlsFound, refCode)
	}
}
