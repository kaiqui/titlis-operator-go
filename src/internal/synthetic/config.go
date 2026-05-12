package synthetic

import (
	"os"

	"gopkg.in/yaml.v3"

	"github.com/titlis/operator/internal/config"
)

type CheckType string

const (
	CheckTypeSiteHealth CheckType = "site_health"
	CheckTypeJSONValue  CheckType = "json_value"
)

type BaseCheck struct {
	Name            string            `yaml:"name"`
	Type            CheckType         `yaml:"type"`
	URL             string            `yaml:"url"`
	IntervalSeconds int               `yaml:"interval_seconds"`
	TimeoutSeconds  float64           `yaml:"timeout_seconds"`
	Tags            map[string]string `yaml:"tags"`
}

type JSONValueCheck struct {
	BaseCheck  `yaml:",inline"`
	JSONPath   string `yaml:"json_path"`
	MetricName string `yaml:"metric_name"`
}

type checksFile struct {
	Checks []rawCheck `yaml:"checks"`
}

type rawCheck struct {
	Name            string            `yaml:"name"`
	Type            CheckType         `yaml:"type"`
	URL             string            `yaml:"url"`
	IntervalSeconds int               `yaml:"interval_seconds"`
	TimeoutSeconds  float64           `yaml:"timeout_seconds"`
	Tags            map[string]string `yaml:"tags"`
	JSONPath        string            `yaml:"json_path"`
	MetricName      string            `yaml:"metric_name"`
}

func LoadChecks(cfg *config.Settings) (siteChecks []BaseCheck, jsonChecks []JSONValueCheck) {
	data, err := os.ReadFile(cfg.SyntheticConfigPath)
	if err != nil {
		// Fallback to legacy single-check env vars
		siteChecks = append(siteChecks, BaseCheck{
			Name:            cfg.SyntheticMonitorName,
			Type:            CheckTypeSiteHealth,
			URL:             cfg.SyntheticMonitorURL,
			IntervalSeconds: cfg.SyntheticIntervalSec,
			TimeoutSeconds:  cfg.SyntheticTimeoutSec,
		})
		return
	}

	var file checksFile
	if err := yaml.Unmarshal(data, &file); err != nil {
		return
	}

	for _, raw := range file.Checks {
		switch raw.Type {
		case CheckTypeJSONValue:
			jsonChecks = append(jsonChecks, JSONValueCheck{
				BaseCheck: BaseCheck{
					Name:            raw.Name,
					Type:            raw.Type,
					URL:             raw.URL,
					IntervalSeconds: raw.IntervalSeconds,
					TimeoutSeconds:  raw.TimeoutSeconds,
					Tags:            raw.Tags,
				},
				JSONPath:   raw.JSONPath,
				MetricName: raw.MetricName,
			})
		default:
			interval := raw.IntervalSeconds
			if interval == 0 {
				interval = 60
			}
			siteChecks = append(siteChecks, BaseCheck{
				Name:            raw.Name,
				Type:            CheckTypeSiteHealth,
				URL:             raw.URL,
				IntervalSeconds: interval,
				TimeoutSeconds:  raw.TimeoutSeconds,
				Tags:            raw.Tags,
			})
		}
	}
	return
}
