// Package controlfile defines the structured files used to export
// organization-owned controls and internal policies out of Openlane and load
// them back in via the API: a controls.yaml inventory, a policies.yaml
// manifest, and one markdown document with YAML frontmatter per policy.
// Serialization is deterministic so repeated exports of unchanged data are
// byte-identical and diff cleanly in git
package controlfile
