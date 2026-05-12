package scorecard

import (
	"os"

	"gopkg.in/yaml.v3"

	"github.com/titlis/operator/internal/model"
)

var DefaultExcludedNamespaces = []string{
	"kube-system", "kube-public", "kube-node-lease",
	"datadog", "titlis-operator", "titlis-system",
}

type rawConfig struct {
	ExcludedNamespaces []string `yaml:"excluded_namespaces"`
}

// LoadConfig reads excluded_namespaces from scorecard-config.yaml.
// All other fields (rules, thresholds) are owned by titlis-scoreops; the
// operator no longer performs scoring, so they are ignored here.
func LoadConfig(path string) model.ScorecardConfig {
	cfg := model.ScorecardConfig{ExcludedNamespaces: DefaultExcludedNamespaces}
	data, err := os.ReadFile(path)
	if err != nil {
		return cfg
	}
	var raw rawConfig
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return cfg
	}
	if len(raw.ExcludedNamespaces) > 0 {
		cfg.ExcludedNamespaces = raw.ExcludedNamespaces
	}
	return cfg
}

// ExclusionFilter decides whether a namespace should be skipped by the operator.
type ExclusionFilter struct {
	namespaces []string
}

func NewExclusionFilter(cfg model.ScorecardConfig) *ExclusionFilter {
	ns := cfg.ExcludedNamespaces
	if len(ns) == 0 {
		ns = DefaultExcludedNamespaces
	}
	return &ExclusionFilter{namespaces: ns}
}

func (f *ExclusionFilter) IsExcluded(ns string) bool {
	for _, e := range f.namespaces {
		if e == ns {
			return true
		}
	}
	return false
}

func (f *ExclusionFilter) ExcludedNamespaces() []string {
	return f.namespaces
}
