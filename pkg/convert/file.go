package convert

import (
	"fmt"
	"io"
	"path/filepath"
	"slices"
	"strings"

	"github.com/theopenlane/courier/pkg/controlfile"
)

// Kind names a document kind an input can convert into, the kinds mirror the
// store files courier keeps in git
type Kind string

const (
	// KindControls converts into the control inventory
	KindControls Kind = "controls"
	// KindPolicies converts into the policy manifest
	KindPolicies Kind = "policies"
)

// csvExtension is the file extension of a CSV input
const csvExtension = ".csv"

// kindSpec wires a document kind to its store file and its converters,
// supporting a new kind means adding one spec to the registry
type kindSpec struct {
	// file is the store file the kind converts into
	file string
	// csv converts a CSV input into the YAML content of the store file
	csv func(r io.Reader, opts CSVOptions) ([]byte, error)
	// merge combines two store files of the kind into one
	merge func(primary, secondary []byte, opts MergeOptions) ([]byte, MergeReport, error)
}

// kindRegistry lists every convertible kind
var kindRegistry = map[Kind]kindSpec{
	KindControls: {
		file:  controlfile.ControlsFile,
		csv:   CSVToYAML[controlfile.Control],
		merge: MergeYAML[controlfile.Control],
	},
	KindPolicies: {
		file:  controlfile.PoliciesFile,
		csv:   CSVToYAML[controlfile.Policy],
		merge: MergeYAML[controlfile.Policy],
	},
}

// Kinds lists every convertible kind in name order
func Kinds() []Kind {
	kinds := make([]Kind, 0, len(kindRegistry))
	for kind := range kindRegistry {
		kinds = append(kinds, kind)
	}

	slices.Sort(kinds)

	return kinds
}

// StoreFile is the store file a kind converts into
func StoreFile(kind Kind) (string, error) {
	spec, ok := kindRegistry[kind]
	if !ok {
		return "", fmt.Errorf("%w: %s", ErrUnknownKind, kind)
	}

	return spec.file, nil
}

// KindForFile resolves the kind an input file name suggests, e.g. a
// controls.csv export converts into the control inventory
func KindForFile(path string) (Kind, bool) {
	name := filepath.Base(path)
	name = strings.TrimSuffix(name, filepath.Ext(name))

	kind := Kind(strings.ToLower(name))
	if _, ok := kindRegistry[kind]; !ok {
		return "", false
	}

	return kind, true
}

// Convert reads an input in the format its file name names and returns the
// YAML content of the store file for kind, only CSV inputs convert today
func Convert(path string, r io.Reader, kind Kind, opts CSVOptions) ([]byte, error) {
	spec, ok := kindRegistry[kind]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrUnknownKind, kind)
	}

	if !strings.EqualFold(filepath.Ext(path), csvExtension) {
		return nil, fmt.Errorf("%w: %s", ErrUnknownFormat, filepath.Ext(path))
	}

	return spec.csv(r, opts)
}

// MergeStoreFiles combines two store files of the same kind, the primary file
// wins field by field and the secondary fills in what it leaves empty
func MergeStoreFiles(kind Kind, primary, secondary []byte, opts MergeOptions) ([]byte, MergeReport, error) {
	spec, ok := kindRegistry[kind]
	if !ok {
		return nil, MergeReport{}, fmt.Errorf("%w: %s", ErrUnknownKind, kind)
	}

	return spec.merge(primary, secondary, opts)
}
