package controlfile

const (
	// ControlsFile is the file name of the control inventory
	ControlsFile = "controls.yaml"

	// PoliciesFile is the file name of the policy manifest
	PoliciesFile = "policies.yaml"

	// PoliciesDir is the directory holding policy markdown documents
	PoliciesDir = "policies"

	// CustomFrameworkKey groups mapped controls that do not derive from a framework
	CustomFrameworkKey = "custom"
)

// MappedControls groups the reference codes of mapped controls by the short
// name of the framework they derive from, organization controls without a
// framework group under CustomFrameworkKey, mirroring the satisfies map used
// in policy frontmatter
type MappedControls map[string][]string

// Control is a single organization-owned control in the inventory, matched to
// Openlane by its ULID when present and by refCode otherwise
type Control struct {
	// ID is the Openlane ULID of the control, empty until the control has been created
	ID string `yaml:"id,omitempty" json:"id,omitempty"`
	// RefCode is the unique reference code of the control within the organization
	RefCode string `yaml:"refCode" json:"refCode" jsonschema:"required,minLength=1"`
	// Title is the human readable title of the control, always rendered so
	// missing values can be filled in directly
	Title string `yaml:"title" json:"title,omitempty"`
	// Description describes what the control is supposed to accomplish,
	// always rendered so missing values can be filled in directly
	Description string `yaml:"description" json:"description,omitempty"`
	// Category is the category of the control, always rendered so missing
	// values can be filled in directly
	Category string `yaml:"category" json:"category,omitempty"`
	// Subcategory is the subcategory of the control, always rendered so
	// missing values can be filled in directly
	Subcategory string `yaml:"subcategory" json:"subcategory,omitempty"`
	// Tags associated with the control
	Tags []string `yaml:"tags,omitempty" json:"tags,omitempty"`
	// MappedControls are the refCodes of controls this control maps to,
	// grouped by framework short name, e.g. SOC 2 to CC1.1
	MappedControls MappedControls `yaml:"mappedControls,omitempty" json:"mappedControls,omitempty"`
	// Subcontrols are the controls nested under this one, they carry the same
	// fields and are created against this control as their parent
	Subcontrols []*Subcontrol `yaml:"subcontrols,omitempty" json:"subcontrols,omitempty"`
}

// Subcontrol is a control nested under an organization control, it holds the
// same authorable fields and exists only in the context of its parent
type Subcontrol struct {
	// ID is the Openlane ULID of the subcontrol, empty until it has been created
	ID string `yaml:"id,omitempty" json:"id,omitempty"`
	// RefCode is the unique reference code of the subcontrol within the organization
	RefCode string `yaml:"refCode" json:"refCode" jsonschema:"required,minLength=1"`
	// Title is the human readable title of the subcontrol, always rendered so
	// missing values can be filled in directly
	Title string `yaml:"title" json:"title,omitempty"`
	// Description describes what the subcontrol is supposed to accomplish,
	// always rendered so missing values can be filled in directly
	Description string `yaml:"description" json:"description,omitempty"`
	// Category is the category of the subcontrol, always rendered so missing
	// values can be filled in directly
	Category string `yaml:"category" json:"category,omitempty"`
	// Subcategory is the subcategory of the subcontrol, always rendered so
	// missing values can be filled in directly
	Subcategory string `yaml:"subcategory" json:"subcategory,omitempty"`
	// Tags associated with the subcontrol
	Tags []string `yaml:"tags,omitempty" json:"tags,omitempty"`
	// MappedControls are the refCodes of controls this subcontrol maps to,
	// grouped by framework short name
	MappedControls MappedControls `yaml:"mappedControls,omitempty" json:"mappedControls,omitempty"`
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
	// MarkdownPath is the store-relative path of the policy markdown document
	MarkdownPath string `yaml:"markdownPath,omitempty" json:"markdownPath,omitempty"`
	// Tags associated with the policy
	Tags []string `yaml:"tags,omitempty" json:"tags,omitempty"`
}

// Frontmatter is the YAML header of a policy markdown document, the server
// parses these fields on upload and applies them to the policy
type Frontmatter struct {
	// OpenlaneID is the Openlane ULID of the policy, used to match the
	// document to its record before the manifest carries an id
	OpenlaneID string `yaml:"openlane_id,omitempty" json:"openlane_id,omitempty"`
	// Title is the policy name
	Title string `yaml:"title,omitempty" json:"title,omitempty"`
	// Status is the document status, e.g. PUBLISHED, DRAFT
	Status string `yaml:"status,omitempty" json:"status,omitempty"`
	// Tags associated with the policy
	Tags []string `yaml:"tags,omitempty" json:"tags,omitempty"`
	// Revision is the document revision, e.g. v1.0.0
	Revision string `yaml:"revision,omitempty" json:"revision,omitempty"`
	// Satisfies lists the framework controls this policy satisfies, grouped
	// by framework short name
	Satisfies MappedControls `yaml:"satisfies,omitempty" json:"satisfies,omitempty"`
}
