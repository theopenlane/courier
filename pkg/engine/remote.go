package engine

import (
	"context"

	"github.com/samber/lo"

	"github.com/theopenlane/core/common/enums"
	"github.com/theopenlane/go-client/graphclient"

	"github.com/theopenlane/courier/pkg/controlfile"
)

// defaultPageSize is the number of records fetched per page
const defaultPageSize = 100

// pageSize is the page size passed to paginated queries
var pageSize = int64(defaultPageSize)

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

// RemoteRef identifies a control participating in a mapping or policy edge
type RemoteRef struct {
	// ID is the Openlane ULID of the referenced control
	ID string `json:"id"`
	// RefCode is the reference code of the referenced control
	RefCode string `json:"refCode"`
	// Framework is the short name of the framework the control derives from,
	// empty for organization custom controls
	Framework string `json:"framework"`
}

// RemoteMapping is a mapped-control record as it exists in the API, only the
// participant edges are needed to derive mappedControls lists
type RemoteMapping struct {
	// ID is the Openlane ULID of the mapped control record
	ID string `json:"id"`
	// From are the controls on the from side of the mapping
	From []RemoteRef `json:"from"`
	// To are the controls on the to side of the mapping
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

// RemoteState is the full set of organization controls, mappings, and
// policies pulled from the API
type RemoteState struct {
	// Controls are the organization-owned controls not derived from a framework
	Controls []RemoteControl
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

		after = page.GetEndCursor()
	}
}

// organizationControlsWhere selects org-owned controls that are user
// manageable, framework-derived and system-owned controls are excluded
func organizationControlsWhere() *graphclient.ControlWhereInput {
	return &graphclient.ControlWhereInput{
		SourceNotIn: []enums.ControlSource{enums.ControlSourceFramework},
		SystemOwned: new(false),
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

// fetchControlsKind retrieves organization controls and their mappings
func fetchControlsKind(ctx context.Context, c *Client, state *RemoteState) error {
	controls, err := c.fetchControls(ctx, organizationControlsWhere())
	if err != nil {
		return err
	}

	mappings, err := c.fetchMappings(ctx)
	if err != nil {
		return err
	}

	state.Controls = controls
	state.Mappings = mappings

	return nil
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
		resp, err := c.typed.GetControls(ctx, &pageSize, nil, after, nil, where, nil)
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
	var ids []string

	where := &graphclient.MappedControlWhereInput{SystemOwned: new(false)}

	err := paginate(func(after *string) (*graphclient.GetMappedControls_MappedControls_PageInfo, error) {
		resp, err := c.typed.GetMappedControls(ctx, &pageSize, nil, after, nil, where, nil)
		if err != nil {
			return nil, err
		}

		for _, edge := range resp.MappedControls.Edges {
			ids = append(ids, edge.GetNode().ID)
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

		to, err := c.mappedToControls(ctx, id)
		if err != nil {
			return nil, err
		}

		mappings = append(mappings, RemoteMapping{ID: id, From: from, To: to})
	}

	return mappings, nil
}

// mappedFromControls pages through the from-side controls of a mapping
func (c *Client) mappedFromControls(ctx context.Context, mappingID string) ([]RemoteRef, error) {
	var refs []RemoteRef

	err := paginate(func(after *string) (*graphclient.GetMappedAllFromControlsForID_MappedControl_FromControls_PageInfo, error) {
		resp, err := c.typed.GetMappedAllFromControlsForID(ctx, mappingID, &pageSize, nil, after, nil, nil)
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
		resp, err := c.typed.GetMappedAllToControlsForID(ctx, mappingID, &pageSize, nil, after, nil, nil)
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
		resp, err := c.typed.GetInternalPolicies(ctx, &pageSize, nil, after, nil, where, nil)
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
