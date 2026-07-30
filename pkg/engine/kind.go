package engine

import (
	"context"

	"github.com/samber/lo"
)

// Kind identifies a syncable object kind
type Kind string

const (
	// KindControls covers organization controls and their control mappings
	KindControls Kind = "controls"
	// KindPolicies covers internal policies, their documents, and their control references
	KindPolicies Kind = "policies"
)

// kindFiles is the store content one kind renders on pull
type kindFiles struct {
	// files maps store-relative paths to rendered content
	files map[string][]byte
	// warnings are records the kind could not represent in the store
	warnings []string
	// cleanup removes store files the kind owns that are no longer
	// present in the rendered set, nil when the kind never removes files
	cleanup func(dir string, files map[string][]byte) ([]string, error)
}

// kindSpec wires one object kind into the pull and apply phases, adding a new
// syncable object means adding one spec to the registry
type kindSpec struct {
	// kind is the name used by the only flag to scope operations
	kind Kind
	// fetch retrieves the kind's remote state
	fetch func(ctx context.Context, c *Client, state *RemoteState) error
	// build renders the kind's store files from remote state
	build func(state *RemoteState) (kindFiles, error)
	// apply pushes the kind's store entries through the API
	apply func(ctx context.Context, c *Client, state *applyState) error
}

// kindRegistry lists every syncable kind in apply order, controls apply
// before policies so control references created in the same run resolve
var kindRegistry = []kindSpec{
	{
		kind:  KindControls,
		fetch: fetchControlsKind,
		build: buildControlsKind,
		apply: applyControlsKind,
	},
	{
		kind:  KindPolicies,
		fetch: fetchPoliciesKind,
		build: buildPoliciesKind,
		apply: applyPoliciesKind,
	},
}

// AllKinds lists every registered kind in apply order
func AllKinds() []Kind {
	return lo.Map(kindRegistry, func(s kindSpec, _ int) Kind { return s.kind })
}

// SelectKinds resolves per-kind flag selections into the kinds to operate
// on, no selection means every kind, order always follows the registry
func SelectKinds(selected map[Kind]bool) []Kind {
	anySelected := lo.SomeBy(AllKinds(), func(k Kind) bool { return selected[k] })
	if !anySelected {
		return AllKinds()
	}

	return lo.Filter(AllKinds(), func(k Kind, _ int) bool { return selected[k] })
}

// scoped returns the registry entries for the selected kinds in apply order
func scoped(kinds []Kind) []kindSpec {
	return lo.Filter(kindRegistry, func(s kindSpec, _ int) bool {
		return lo.Contains(kinds, s.kind)
	})
}
