package scorecard_test

import (
	"os"
	"testing"

	"github.com/titlis/operator/internal/scorecard"
)

func TestNewExclusionFilter_DefaultNamespaces(t *testing.T) {
	f := scorecard.NewExclusionFilter(scorecard.LoadConfig("/nonexistent/path.yaml"))

	if !f.IsExcluded("kube-system") {
		t.Error("kube-system should be excluded by default")
	}
	if !f.IsExcluded("datadog") {
		t.Error("datadog should be excluded by default")
	}
	if f.IsExcluded("production") {
		t.Error("production should not be excluded")
	}
}

func TestExclusionFilter_ExcludedNamespaces_ReturnsList(t *testing.T) {
	f := scorecard.NewExclusionFilter(scorecard.LoadConfig("/nonexistent/path.yaml"))
	ns := f.ExcludedNamespaces()

	if len(ns) == 0 {
		t.Error("expected at least one excluded namespace")
	}
	found := false
	for _, n := range ns {
		if n == "kube-system" {
			found = true
		}
	}
	if !found {
		t.Error("kube-system not in ExcludedNamespaces()")
	}
}

func TestLoadConfig_FromFile(t *testing.T) {
	f, err := os.CreateTemp("", "scorecard-config-*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())
	f.WriteString("excluded_namespaces:\n  - monitoring\n  - logging\n")
	f.Close()

	cfg := scorecard.LoadConfig(f.Name())
	f2 := scorecard.NewExclusionFilter(cfg)

	if !f2.IsExcluded("monitoring") {
		t.Error("monitoring should be excluded from config file")
	}
	if !f2.IsExcluded("logging") {
		t.Error("logging should be excluded from config file")
	}
	if f2.IsExcluded("kube-system") {
		t.Error("kube-system should not be excluded (overridden by config file)")
	}
}

func TestLoadConfig_MissingFile_UsesDefaults(t *testing.T) {
	cfg := scorecard.LoadConfig("/nonexistent/config.yaml")
	f := scorecard.NewExclusionFilter(cfg)

	if !f.IsExcluded("kube-system") {
		t.Error("should use default exclusions when config file is missing")
	}
}
