package controlfile

// Normalize fills schema defaults for a policy entry, the markdown path
// derives from the name when unset, data values are never modified
func (p *Policy) Normalize() {
	if p.MarkdownPath == "" && p.Name != "" {
		p.MarkdownPath = PolicyMarkdownPath(p.Name)
	}
}

// NormalizePolicies fills schema defaults for every entry
func NormalizePolicies(policies []*Policy) {
	for _, p := range policies {
		p.Normalize()
	}
}
