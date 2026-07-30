package engine

import "context"

// PullResult summarizes what a pull wrote to the store
type PullResult struct {
	// TotalControls is the number of organization controls exported
	TotalControls int `json:"totalControls"`
	// TotalPolicies is the number of internal policies exported
	TotalPolicies int `json:"totalPolicies"`
	// Written are the relative paths created or updated
	Written []string `json:"written,omitempty"`
	// Removed are the relative paths of stale documents deleted
	Removed []string `json:"removed,omitempty"`
	// Warnings are records the export could not represent, such as a
	// subcontrol whose parent control is not itself exported
	Warnings []string `json:"warnings,omitempty"`
}

// Pull exports the selected kinds into the store, rewriting each kind's
// files from the server's current state and removing stale documents
func (c *Client) Pull(ctx context.Context, dir string, kinds []Kind) (*PullResult, error) {
	state, err := c.FetchState(ctx, kinds)
	if err != nil {
		return nil, err
	}

	result := &PullResult{
		TotalControls: len(state.Controls),
		TotalPolicies: len(state.Policies),
	}

	for _, spec := range scoped(kinds) {
		rendered, err := spec.build(state)
		if err != nil {
			return nil, err
		}

		written, removed, err := writeKindFiles(dir, rendered)
		if err != nil {
			return nil, err
		}

		result.Written = append(result.Written, written...)
		result.Removed = append(result.Removed, removed...)
		result.Warnings = append(result.Warnings, rendered.warnings...)
	}

	return result, nil
}
