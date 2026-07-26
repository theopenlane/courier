package engine

import "context"

// PullResult summarizes what a pull wrote to the workspace
type PullResult struct {
	// TotalControls is the number of organization controls exported
	TotalControls int `json:"totalControls"`
	// TotalPolicies is the number of internal policies exported
	TotalPolicies int `json:"totalPolicies"`
	// Written are the relative paths created or updated
	Written []string `json:"written,omitempty"`
	// Removed are the relative paths of stale documents deleted
	Removed []string `json:"removed,omitempty"`
}

// Pull exports all organization controls, mappings, and policies into the
// workspace, rewriting files in canonical form and removing stale markdown
func (c *Client) Pull(ctx context.Context, dir string) (*PullResult, error) {
	state, err := c.FetchState(ctx)
	if err != nil {
		return nil, err
	}

	controls := BuildControls(state)

	policies, markdown, err := BuildPolicies(state)
	if err != nil {
		return nil, err
	}

	written, removed, err := WriteWorkspace(dir, controls, policies, markdown)
	if err != nil {
		return nil, err
	}

	return &PullResult{
		TotalControls: len(controls),
		TotalPolicies: len(policies),
		Written:       written,
		Removed:       removed,
	}, nil
}
