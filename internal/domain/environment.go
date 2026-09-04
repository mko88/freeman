package domain

// Environment is a named set of variables (e.g. "Development", "Staging"),
// persisted as one environments/<slug>.json file. Variables marked Secret
// carry no Value in that tracked file — the real value lives in a sibling
// <slug>.local.json override that store merges in at load time.
type Environment struct {
	FormatVersion string     `json:"formatVersion"`
	ID            string     `json:"id"`
	Name          string     `json:"name"`
	Variables     []Variable `json:"variables"`
}

type Variable struct {
	Key     string `json:"key"`
	Value   string `json:"value"`
	Enabled bool   `json:"enabled"`
	Secret  bool   `json:"secret"`
}

// ResolvedVariables returns the enabled variables as a plain map, for use
// by httpengine.Substitute.
func (e Environment) ResolvedVariables() map[string]string {
	vars := make(map[string]string, len(e.Variables))
	for _, v := range e.Variables {
		if v.Enabled {
			vars[v.Key] = v.Value
		}
	}
	return vars
}
