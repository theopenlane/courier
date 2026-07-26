package controlfile

const (
	// ControlsFile is the file name of the control inventory
	ControlsFile = "controls.yaml"

	// PoliciesFile is the file name of the policy manifest
	PoliciesFile = "policies.yaml"

	// PoliciesDir is the directory holding policy markdown documents
	PoliciesDir = "policies"
)

// Control is a single organization-owned control in the inventory, matched to
// Openlane by its ULID when present and by refCode otherwise
type Control struct {
	// ID is the Openlane ULID of the control, empty until the control has been created
	ID string `yaml:"id,omitempty" json:"id,omitempty"`
	// RefCode is the unique reference code of the control within the organization
	RefCode string `yaml:"refCode" json:"refCode" jsonschema:"required,minLength=1"`
	// Title is the human readable title of the control
	Title string `yaml:"title,omitempty" json:"title,omitempty"`
	// Description describes what the control is supposed to accomplish
	Description string `yaml:"description,omitempty" json:"description,omitempty"`
	// Category is the category of the control
	Category string `yaml:"category,omitempty" json:"category,omitempty"`
	// Subcategory is the subcategory of the control
	Subcategory string `yaml:"subcategory,omitempty" json:"subcategory,omitempty"`
	// Tags associated with the control
	Tags []string `yaml:"tags,omitempty" json:"tags,omitempty"`
	// MappedControls are the refCodes of controls this control maps to,
	// typically framework controls cloned into the organization such as CC1.1
	MappedControls []string `yaml:"mappedControls,omitempty" json:"mappedControls,omitempty"`
}

// Policy is a single internal policy in the manifest, its body lives in the
// markdown document referenced by MarkdownPath
type Policy struct {
	// ID is the Openlane ULID of the policy, empty until the policy has been created
	ID string `yaml:"id,omitempty" json:"id,omitempty"`
	// Name is the unique name of the policy
	Name string `yaml:"name" json:"name" jsonschema:"required,minLength=1"`
	// PolicyType is the kind of policy, e.g. Security, Operational
	PolicyType string `yaml:"policyType,omitempty" json:"policyType,omitempty"`
	// MarkdownPath is the workspace-relative path of the policy markdown document
	MarkdownPath string `yaml:"markdownPath,omitempty" json:"markdownPath,omitempty"`
	// Tags associated with the policy
	Tags []string `yaml:"tags,omitempty" json:"tags,omitempty"`
	// MappedControls are the refCodes of controls this policy satisfies
	MappedControls []string `yaml:"mappedControls,omitempty" json:"mappedControls,omitempty"`
}

// Frontmatter is the YAML header of a policy markdown document, the server
// parses these fields on upload and applies them to the policy
type Frontmatter struct {
	// Title is the policy name
	Title string `yaml:"title,omitempty" json:"title,omitempty"`
	// Status is the document status, e.g. PUBLISHED, DRAFT
	Status string `yaml:"status,omitempty" json:"status,omitempty"`
	// Revision is the document revision, e.g. v1.0.0
	Revision string `yaml:"revision,omitempty" json:"revision,omitempty"`
	// Tags associated with the policy
	Tags []string `yaml:"tags,omitempty" json:"tags,omitempty"`
}
