package engine

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/samber/lo"

	"github.com/theopenlane/courier/pkg/controlfile"
)

const (
	// dirPerm is the permission mode for created directories
	dirPerm = 0o755
	// filePerm is the permission mode for written files
	filePerm = 0o644
)

// Store is the parsed content of an export directory: the control
// inventory, the policy manifest, and the raw policy markdown documents
// keyed by store-relative path
type Store struct {
	// Dir is the directory the files were loaded from
	Dir string
	// Controls is the parsed control inventory
	Controls []*controlfile.Control
	// Policies is the parsed policy manifest
	Policies []*controlfile.Policy
	// PolicyMarkdown holds the raw markdown document per manifest entry
	PolicyMarkdown map[string][]byte
}

// LoadFile parses a single YAML file and resolves whether it holds controls
// or a policy manifest by validating against the document schemas, the file
// name carries no meaning. Policy documents resolve relative to the file's
// directory
func LoadFile(path string) (*Store, []Kind, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}

	dir := filepath.Dir(path)

	if controls, err := controlfile.Unmarshal[controlfile.Control](data); err == nil && len(controls) > 0 {
		if err := controlfile.Validate(controls); err == nil {
			return &Store{Dir: dir, Controls: controls, PolicyMarkdown: map[string][]byte{}}, []Kind{KindControls}, nil
		}
	}

	if policies, err := controlfile.Unmarshal[controlfile.Policy](data); err == nil && len(policies) > 0 {
		if err := controlfile.Validate(policies); err == nil {
			store := &Store{Dir: dir, Policies: policies, PolicyMarkdown: map[string][]byte{}}
			controlfile.NormalizePolicies(store.Policies)

			if err := loadPolicyDocuments(store); err != nil {
				return nil, nil, err
			}

			return store, []Kind{KindPolicies}, nil
		}
	}

	return nil, nil, fmtErr(ErrUnrecognizedFile, path)
}

// loadPolicyDocuments reads the markdown document for every manifest entry
func loadPolicyDocuments(store *Store) error {
	for _, policy := range store.Policies {
		markdown, err := os.ReadFile(filepath.Join(store.Dir, filepath.FromSlash(policy.MarkdownPath)))
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return fmtErr(ErrMissingMarkdown, policy.MarkdownPath)
			}

			return err
		}

		store.PolicyMarkdown[policy.MarkdownPath] = markdown
	}

	return nil
}

// NewStore parses controls.yaml, policies.yaml, and every referenced
// markdown document under dir, missing files load as empty
func NewStore(dir string) (*Store, error) {
	store := &Store{Dir: dir, PolicyMarkdown: map[string][]byte{}}

	data, err := readOptional(filepath.Join(dir, controlfile.ControlsFile))
	if err != nil {
		return nil, err
	}

	if data != nil {
		if store.Controls, err = controlfile.Unmarshal[controlfile.Control](data); err != nil {
			return nil, err
		}
	}

	data, err = readOptional(filepath.Join(dir, controlfile.PoliciesFile))
	if err != nil {
		return nil, err
	}

	if data != nil {
		if store.Policies, err = controlfile.Unmarshal[controlfile.Policy](data); err != nil {
			return nil, err
		}
	}

	controlfile.NormalizePolicies(store.Policies)

	if err := loadPolicyDocuments(store); err != nil {
		return nil, err
	}

	return store, nil
}

// policyMarkdown returns the raw markdown document, its frontmatter, and its
// body for a manifest entry
func (store *Store) policyMarkdown(policy *controlfile.Policy) ([]byte, controlfile.Frontmatter, string, error) {
	markdown := store.PolicyMarkdown[policy.MarkdownPath]

	fm, body, err := controlfile.SplitPolicyMarkdown(markdown)
	if err != nil {
		return nil, controlfile.Frontmatter{}, "", err
	}

	return markdown, fm, body, nil
}

// writeKindFiles writes one kind's rendered files to dir and runs the
// kind's stale file cleanup, returning the relative paths written and removed
func writeKindFiles(dir string, rendered kindFiles) (written, removed []string, err error) {
	paths := lo.Keys(rendered.files)
	slices.Sort(paths)

	for _, rel := range paths {
		wrote, err := writeIfChanged(filepath.Join(dir, filepath.FromSlash(rel)), rendered.files[rel])
		if err != nil {
			return nil, nil, err
		}

		if wrote {
			written = append(written, rel)
		}
	}

	if rendered.cleanup != nil {
		if removed, err = rendered.cleanup(dir, rendered.files); err != nil {
			return nil, nil, err
		}
	}

	return written, removed, nil
}

// removeStaleMarkdown deletes markdown files under the policies directory
// that are not part of the current export
func removeStaleMarkdown(dir string, rendered map[string][]byte) ([]string, error) {
	policiesDir := filepath.Join(dir, controlfile.PoliciesDir)

	entries, err := os.ReadDir(policiesDir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}

	if err != nil {
		return nil, err
	}

	var removed []string

	for _, entry := range entries {
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), controlfile.MarkdownExtension) {
			continue
		}

		rel := controlfile.PoliciesDir + "/" + entry.Name()
		if _, ok := rendered[rel]; ok {
			continue
		}

		if err := os.Remove(filepath.Join(policiesDir, entry.Name())); err != nil {
			return nil, err
		}

		removed = append(removed, rel)
	}

	slices.Sort(removed)

	return removed, nil
}

// FormatResult reports what Format changed or would change
type FormatResult struct {
	// Changed are the relative paths whose content differs from canonical form
	Changed []string
}

// Format rewrites controls.yaml and policies.yaml into canonical form and
// validates both, markdown documents are left untouched. With check set the
// files are not rewritten
func Format(dir string, check bool) (*FormatResult, error) {
	store, err := NewStore(dir)
	if err != nil {
		return nil, err
	}

	if err := controlfile.Validate(store.Controls); err != nil {
		return nil, err
	}

	if err := controlfile.Validate(store.Policies); err != nil {
		return nil, err
	}

	canonical := map[string][]byte{}

	if canonical[controlfile.ControlsFile], err = controlfile.Marshal(store.Controls); err != nil {
		return nil, err
	}

	if canonical[controlfile.PoliciesFile], err = controlfile.Marshal(store.Policies); err != nil {
		return nil, err
	}

	empty := map[string]bool{
		controlfile.ControlsFile: len(store.Controls) == 0,
		controlfile.PoliciesFile: len(store.Policies) == 0,
	}

	result := &FormatResult{}

	for _, rel := range []string{controlfile.ControlsFile, controlfile.PoliciesFile} {
		target := filepath.Join(dir, rel)

		current, err := readOptional(target)
		if err != nil {
			return nil, err
		}

		if current == nil && empty[rel] {
			continue
		}

		if bytes.Equal(current, canonical[rel]) {
			continue
		}

		result.Changed = append(result.Changed, rel)

		if check {
			continue
		}

		if _, err := writeIfChanged(target, canonical[rel]); err != nil {
			return nil, err
		}
	}

	return result, nil
}

// writeIfChanged writes data to path when the current content differs,
// creating parent directories as needed, and reports whether it wrote
func writeIfChanged(path string, data []byte) (bool, error) {
	current, err := os.ReadFile(path)
	if err == nil && bytes.Equal(current, data) {
		return false, nil
	}

	if err := os.MkdirAll(filepath.Dir(path), dirPerm); err != nil {
		return false, err
	}

	if err := os.WriteFile(path, data, filePerm); err != nil {
		return false, err
	}

	return true, nil
}

// readOptional reads a file, a missing file returns nil data and no error
func readOptional(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}

	if err != nil {
		return nil, err
	}

	return data, nil
}
