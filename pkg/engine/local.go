package engine

import (
	"bytes"
	"errors"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/samber/lo"

	"github.com/theopenlane/courier/pkg/controlfile"
)

const (
	dirPerm  = 0o755
	filePerm = 0o644
)

// Workspace is the parsed content of an export directory: the control
// inventory, the policy manifest, and the raw policy markdown documents
// keyed by workspace-relative path
type Workspace struct {
	// Controls is the parsed control inventory
	Controls []*controlfile.Control
	// Policies is the parsed policy manifest
	Policies []*controlfile.Policy
	// PolicyMarkdown holds the raw markdown document per manifest entry
	PolicyMarkdown map[string][]byte
}

// LoadWorkspace parses controls.yaml, policies.yaml, and every referenced
// markdown document under dir, missing files load as empty
func LoadWorkspace(dir string) (*Workspace, error) {
	ws := &Workspace{PolicyMarkdown: map[string][]byte{}}

	data, err := readOptional(filepath.Join(dir, controlfile.ControlsFile))
	if err != nil {
		return nil, err
	}

	if data != nil {
		if ws.Controls, err = controlfile.UnmarshalControls(data); err != nil {
			return nil, err
		}
	}

	data, err = readOptional(filepath.Join(dir, controlfile.PoliciesFile))
	if err != nil {
		return nil, err
	}

	if data != nil {
		if ws.Policies, err = controlfile.UnmarshalPolicies(data); err != nil {
			return nil, err
		}
	}

	controlfile.NormalizePolicies(ws.Policies)

	for _, policy := range ws.Policies {
		markdown, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(policy.MarkdownPath)))
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil, fmtErr(ErrMissingMarkdown, policy.MarkdownPath)
			}

			return nil, err
		}

		ws.PolicyMarkdown[policy.MarkdownPath] = markdown
	}

	return ws, nil
}

// policyMarkdown returns the raw markdown document and its body for a
// manifest entry
func (ws *Workspace) policyMarkdown(policy *controlfile.Policy) ([]byte, string, error) {
	markdown := ws.PolicyMarkdown[policy.MarkdownPath]

	_, body, err := controlfile.SplitPolicyMarkdown(markdown)
	if err != nil {
		return nil, "", err
	}

	return markdown, body, nil
}

// WriteWorkspace writes the inventory, manifest, and markdown documents to
// dir and removes markdown files under the policies directory that are no
// longer referenced, returning the relative paths written and removed
func WriteWorkspace(dir string, controls []*controlfile.Control, policies []*controlfile.Policy, markdown map[string][]byte) (written, removed []string, err error) {
	controlsData, err := controlfile.MarshalControls(controls)
	if err != nil {
		return nil, nil, err
	}

	policiesData, err := controlfile.MarshalPolicies(policies)
	if err != nil {
		return nil, nil, err
	}

	files := map[string][]byte{
		controlfile.ControlsFile: controlsData,
		controlfile.PoliciesFile: policiesData,
	}

	maps.Copy(files, markdown)

	paths := lo.Keys(files)
	slices.Sort(paths)

	for _, rel := range paths {
		wrote, err := writeIfChanged(filepath.Join(dir, filepath.FromSlash(rel)), files[rel])
		if err != nil {
			return nil, nil, err
		}

		if wrote {
			written = append(written, rel)
		}
	}

	removed, err = removeStaleMarkdown(dir, markdown)
	if err != nil {
		return nil, nil, err
	}

	return written, removed, nil
}

// removeStaleMarkdown deletes markdown files under the policies directory
// that are not part of the current export
func removeStaleMarkdown(dir string, markdown map[string][]byte) ([]string, error) {
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
		if _, ok := markdown[rel]; ok {
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
	ws, err := LoadWorkspace(dir)
	if err != nil {
		return nil, err
	}

	if err := controlfile.ValidateControls(ws.Controls); err != nil {
		return nil, err
	}

	if err := controlfile.ValidatePolicies(ws.Policies); err != nil {
		return nil, err
	}

	canonical := map[string][]byte{}

	if canonical[controlfile.ControlsFile], err = controlfile.MarshalControls(ws.Controls); err != nil {
		return nil, err
	}

	if canonical[controlfile.PoliciesFile], err = controlfile.MarshalPolicies(ws.Policies); err != nil {
		return nil, err
	}

	empty := map[string]bool{
		controlfile.ControlsFile: len(ws.Controls) == 0,
		controlfile.PoliciesFile: len(ws.Policies) == 0,
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
