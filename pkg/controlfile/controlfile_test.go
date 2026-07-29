package controlfile_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/theopenlane/courier/pkg/controlfile"
)

func testControls() []*controlfile.Control {
	return []*controlfile.Control{
		{
			ID:          "CTL_01ABC",
			RefCode:     "CC1.1.3",
			Description: "New hires are required to complete an acknowledgment form upon hire",
			Category:    "Control Environment",
			Subcategory: "Integrity and Ethics",
			MappedControls: controlfile.MappedControls{
				"SOC 2": {"CC1.1"},
			},
		},
		{
			RefCode:  "CC1.1.1",
			Category: "Control Environment",
			Tags:     []string{"security", "handbook"},
		},
	}
}

func TestControlsRoundTrip(t *testing.T) {
	data, err := controlfile.Marshal(testControls())
	require.NoError(t, err)

	parsed, err := controlfile.Unmarshal[controlfile.Control](data)
	require.NoError(t, err)
	require.NoError(t, controlfile.Validate(parsed))

	// data round-trips verbatim: no trimming, reordering, or deduplication
	assert.Equal(t, testControls(), parsed)

	// re-marshaling parsed data is byte-stable
	again, err := controlfile.Marshal(parsed)
	require.NoError(t, err)
	assert.Equal(t, string(data), string(again))
}

func TestValidateControls(t *testing.T) {
	require.NoError(t, controlfile.Validate(testControls()))

	// a missing refCode fails schema validation, uniqueness is enforced by the API
	controls := testControls()
	controls[1].RefCode = ""

	data, err := controlfile.Marshal(controls)
	require.NoError(t, err)

	parsed, err := controlfile.Unmarshal[controlfile.Control](data)
	require.NoError(t, err)
	assert.ErrorIs(t, controlfile.Validate(parsed), controlfile.ErrSchemaValidation)
}

func TestPoliciesRoundTrip(t *testing.T) {
	policies := []*controlfile.Policy{
		{
			Name:         "Application Security Policy",
			PolicyType:   "Security",
			MarkdownPath: "data/demo/policies/application.md",
			Tags:         []string{"application", "security", "ASP"},
		},
		{
			Name: "Availability Policy",
		},
	}

	data, err := controlfile.Marshal(policies)
	require.NoError(t, err)

	parsed, err := controlfile.Unmarshal[controlfile.Policy](data)
	require.NoError(t, err)
	require.NoError(t, controlfile.Validate(parsed))

	require.Len(t, parsed, 2)
	assert.Equal(t, "Application Security Policy", parsed[0].Name)
	assert.Equal(t, "data/demo/policies/application.md", parsed[0].MarkdownPath)

	// a missing markdownPath defaults to the derived path on normalize
	controlfile.NormalizePolicies(parsed)
	assert.Equal(t, "policies/availability-policy.md", parsed[1].MarkdownPath)
}

func TestValidatePolicies(t *testing.T) {
	require.NoError(t, controlfile.Validate([]*controlfile.Policy{{Name: "Access Policy"}}))

	// a missing name fails schema validation
	data, err := controlfile.Marshal([]*controlfile.Policy{{PolicyType: "Security"}})
	require.NoError(t, err)

	parsed, err := controlfile.Unmarshal[controlfile.Policy](data)
	require.NoError(t, err)
	assert.ErrorIs(t, controlfile.Validate(parsed), controlfile.ErrSchemaValidation)
}

func TestPolicyMarkdownRoundTrip(t *testing.T) {
	fm := controlfile.Frontmatter{
		Title: "Access Onboarding and Termination Policy",
		Tags:  []string{"security", "access"},
	}
	body := "# Purpose\n\nThis policy defines access onboarding."

	doc, err := controlfile.MarshalPolicyMarkdown(fm, body)
	require.NoError(t, err)

	parsedFM, parsedBody, err := controlfile.SplitPolicyMarkdown(doc)
	require.NoError(t, err)

	assert.Equal(t, fm.Title, parsedFM.Title)
	assert.Equal(t, []string{"security", "access"}, parsedFM.Tags)
	assert.Equal(t, body, parsedBody)

	// documents without frontmatter parse as body only
	_, plainBody, err := controlfile.SplitPolicyMarkdown([]byte("just a body\n"))
	require.NoError(t, err)
	assert.Equal(t, "just a body", plainBody)
}

func TestPolicyMarkdownPath(t *testing.T) {
	assert.Equal(t, "policies/application-security-policy.md", controlfile.PolicyMarkdownPath("Application Security Policy"))
}
