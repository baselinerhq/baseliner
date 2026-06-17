package checks

// Registry maps check IDs to Check implementations.
type Registry struct {
	m map[string]Check
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{m: make(map[string]Check)}
}

// Register adds a check, keyed by its ID.
func (r *Registry) Register(c Check) {
	r.m[c.ID()] = c
}

// Get returns the check for an ID, and whether it was found.
func (r *Registry) Get(id string) (Check, bool) {
	c, ok := r.m[id]
	return c, ok
}

// BuildDefault returns a registry with all 10 built-in checks registered.
func BuildDefault() *Registry {
	r := NewRegistry()
	for _, c := range []Check{
		readmeExists{base{"readme_exists", LayerFS}},
		readmeNonEmpty{base{"readme_nonempty", LayerFS}},
		readmeHasHeading{base{"readme_has_heading", LayerFS}},
		licenseExists{base{"license_exists", LayerFS}},
		gitignoreExists{base{"gitignore_exists", LayerFS}},
		ciPresent{base{"ci_present", LayerFS}},
		codeownersExists{base{"codeowners_exists", LayerFS}},
		dependencyUpdateConfig{base{"dependency_update_config", LayerFS}},
		defaultBranchIsMain{base{"default_branch_is_main", LayerGit}},
		staleRepo{base{"stale_repo", LayerGit}},
	} {
		r.Register(c)
	}
	return r
}
