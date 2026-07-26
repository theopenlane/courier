package engine

import (
	"bytes"
	"context"
	"fmt"
	"path"
	"strings"

	"github.com/99designs/gqlgen/graphql"
	"github.com/samber/lo"

	"github.com/theopenlane/core/common/enums"
	"github.com/theopenlane/go-client/graphclient"
)

// markdownContentType is the content type used for policy uploads
const markdownContentType = "text/markdown"

// ApplyResult summarizes what an apply changed in the API
type ApplyResult struct {
	// CreatedControls are the refCodes of controls created, in order
	CreatedControls []string `json:"createdControls,omitempty"`
	// UpdatedControls are the refCodes of controls updated, in order
	UpdatedControls []string `json:"updatedControls,omitempty"`
	// CreatedMappings counts mapped controls created
	CreatedMappings int `json:"createdMappings"`
	// CreatedPolicies are the names of policies created, in order
	CreatedPolicies []string `json:"createdPolicies,omitempty"`
	// UpdatedPolicies are the names of policies updated, in order
	UpdatedPolicies []string `json:"updatedPolicies,omitempty"`
	// Warnings are non-fatal issues such as mapped control refCodes that
	// could not be resolved and were skipped
	Warnings []string `json:"warnings,omitempty"`
}

// Apply executes a plan against the API: controls first so mapped control
// references to new controls resolve, then mappings, then policies, nothing
// is ever deleted
func (c *Client) Apply(ctx context.Context, plan *Plan) (*ApplyResult, error) {
	result := &ApplyResult{}
	resolved := map[string]string{}

	for _, create := range plan.CreateControls {
		created, err := c.applyControlCreate(ctx, plan, create)
		if err != nil {
			return result, fmt.Errorf("creating control %q: %w", create.Doc.RefCode, err)
		}

		if created {
			result.CreatedControls = append(result.CreatedControls, create.Doc.RefCode)
		} else {
			result.Warnings = append(result.Warnings, fmt.Sprintf("control %q already exists, skipped create", create.Doc.RefCode))
		}
	}

	for _, update := range plan.UpdateControls {
		if err := c.applyControlUpdate(ctx, update); err != nil {
			return result, fmt.Errorf("updating control %q: %w", update.Doc.RefCode, err)
		}

		result.UpdatedControls = append(result.UpdatedControls, update.Doc.RefCode)
	}

	for _, add := range plan.MappingAdds {
		created, err := c.applyMappingAdd(ctx, plan, add, resolved, result)
		if err != nil {
			return result, fmt.Errorf("mapping control %q: %w", add.RefCode, err)
		}

		if created {
			result.CreatedMappings++
		}
	}

	for _, create := range plan.CreatePolicies {
		created, err := c.applyPolicyCreate(ctx, plan, create, resolved, result)
		if err != nil {
			return result, fmt.Errorf("creating policy %q: %w", create.Policy.Name, err)
		}

		if created {
			result.CreatedPolicies = append(result.CreatedPolicies, create.Policy.Name)
		} else {
			result.Warnings = append(result.Warnings, fmt.Sprintf("policy %q already exists, skipped create", create.Policy.Name))
		}
	}

	for _, update := range plan.UpdatePolicies {
		if err := c.applyPolicyUpdate(ctx, plan, update, resolved, result); err != nil {
			return result, fmt.Errorf("updating policy %q: %w", update.Policy.Name, err)
		}

		result.UpdatedPolicies = append(result.UpdatedPolicies, update.Policy.Name)
	}

	return result, nil
}

// applyControlCreate creates a control and records its new ID for later
// mapped control resolution, an already existing control is skipped
func (c *Client) applyControlCreate(ctx context.Context, plan *Plan, create ControlCreate) (bool, error) {
	doc := create.Doc

	input := graphclient.CreateControlInput{
		RefCode:     doc.RefCode,
		Title:       lo.EmptyableToPtr(doc.Title),
		Description: lo.EmptyableToPtr(doc.Description),
		Category:    lo.EmptyableToPtr(doc.Category),
		Subcategory: lo.EmptyableToPtr(doc.Subcategory),
		Tags:        doc.Tags,
		Status:      &enums.ControlStatusApproved,
	}

	resp, err := c.typed.CreateControl(ctx, input)
	if err != nil {
		if isAlreadyExists(err) {
			return false, nil
		}

		return false, err
	}

	doc.ID = resp.CreateControl.Control.ID
	plan.localIDs[strings.ToLower(doc.RefCode)] = doc.ID

	return true, nil
}

// applyControlUpdate sends the entry's fields, empty values are omitted and
// never clear the corresponding API field
func (c *Client) applyControlUpdate(ctx context.Context, update ControlUpdate) error {
	doc := update.Doc

	input := graphclient.UpdateControlInput{
		Title:       lo.EmptyableToPtr(doc.Title),
		Description: lo.EmptyableToPtr(doc.Description),
		Category:    lo.EmptyableToPtr(doc.Category),
		Subcategory: lo.EmptyableToPtr(doc.Subcategory),
	}

	if doc.RefCode != update.Remote.RefCode {
		input.RefCode = &doc.RefCode
	}

	if len(doc.Tags) > 0 {
		input.Tags = doc.Tags
	}

	_, err := c.typed.UpdateControl(ctx, update.Remote.ID, input)

	return err
}

// applyMappingAdd creates one mapped control from the source control to every
// resolvable target, unresolvable targets are skipped with a warning
func (c *Client) applyMappingAdd(ctx context.Context, plan *Plan, add MappingAdd, resolved map[string]string, result *ApplyResult) (bool, error) {
	controlID := add.ControlID
	if controlID == "" {
		controlID = plan.localIDs[strings.ToLower(add.RefCode)]
	}

	if controlID == "" {
		result.Warnings = append(result.Warnings, fmt.Sprintf("control %q has no ID, skipping its mappings", add.RefCode))

		return false, nil
	}

	toIDs, err := c.resolveTargets(ctx, add.Targets, plan, resolved, result)
	if err != nil {
		return false, err
	}

	if len(toIDs) == 0 {
		return false, nil
	}

	input := graphclient.CreateMappedControlInput{
		FromControlIDs: []string{controlID},
		ToControlIDs:   toIDs,
		Source:         &enums.MappingSourceImported,
	}

	_, err = c.typed.CreateMappedControl(ctx, input)

	return err == nil, err
}

// applyPolicyCreate uploads the markdown document and links kind and
// controls, an already existing policy is skipped
func (c *Client) applyPolicyCreate(ctx context.Context, plan *Plan, create PolicyCreate, resolved map[string]string, result *ApplyResult) (bool, error) {
	policy := create.Policy

	var ownerID *string
	if c.config.OrganizationID != "" {
		ownerID = &c.config.OrganizationID
	}

	resp, err := c.typed.CreateUploadInternalPolicy(ctx, markdownUpload(policy.MarkdownPath, create.Markdown), ownerID)
	if err != nil {
		if isAlreadyExists(err) {
			return false, nil
		}

		return false, err
	}

	policy.ID = resp.CreateUploadInternalPolicy.InternalPolicy.ID

	input := graphclient.UpdateInternalPolicyInput{
		InternalPolicyKindName: lo.EmptyableToPtr(policy.PolicyType),
	}

	if input.AddControlIDs, err = c.resolveTargets(ctx, policy.MappedControls, plan, resolved, result); err != nil {
		return false, err
	}

	if _, err = c.typed.UpdateInternalPolicy(ctx, policy.ID, input); err != nil {
		return false, err
	}

	return true, nil
}

// applyPolicyUpdate sends the entry's metadata with empty values omitted,
// re-uploads the markdown when the body changed, and links newly mapped
// controls
func (c *Client) applyPolicyUpdate(ctx context.Context, plan *Plan, update PolicyUpdate, resolved map[string]string, result *ApplyResult) error {
	policy := update.Policy

	input := graphclient.UpdateInternalPolicyInput{
		Name:                   &policy.Name,
		InternalPolicyKindName: lo.EmptyableToPtr(policy.PolicyType),
	}

	if len(policy.Tags) > 0 {
		input.Tags = policy.Tags
	}

	var err error
	if input.AddControlIDs, err = c.resolveTargets(ctx, update.AddControls, plan, resolved, result); err != nil {
		return err
	}

	if update.BodyChanged {
		_, err = c.typed.UpdateInternalPolicyWithFile(ctx, update.Remote.ID, markdownUpload(policy.MarkdownPath, update.Markdown), input)

		return err
	}

	_, err = c.typed.UpdateInternalPolicy(ctx, update.Remote.ID, input)

	return err
}

// resolveTargets resolves mapped control refCodes to IDs using workspace
// controls first and an API lookup otherwise, unresolvable refCodes are
// skipped with a warning
func (c *Client) resolveTargets(ctx context.Context, targets []string, plan *Plan, resolved map[string]string, result *ApplyResult) ([]string, error) {
	ids := make([]string, 0, len(targets))

	for _, target := range targets {
		key := strings.ToLower(target)

		if id, ok := plan.localIDs[key]; ok && id != "" {
			ids = append(ids, id)

			continue
		}

		id, ok := resolved[key]
		if !ok {
			var err error

			id, err = c.ResolveControlRefCode(ctx, target)
			if err != nil {
				return nil, err
			}

			resolved[key] = id
		}

		if id == "" {
			result.Warnings = append(result.Warnings, fmt.Sprintf("control %q not found, skipping mapping target", target))

			continue
		}

		ids = append(ids, id)
	}

	return ids, nil
}

// markdownUpload wraps a markdown document as a GraphQL upload
func markdownUpload(relPath string, data []byte) graphql.Upload {
	return graphql.Upload{
		File:        bytes.NewReader(data),
		Filename:    path.Base(relPath),
		Size:        int64(len(data)),
		ContentType: markdownContentType,
	}
}
