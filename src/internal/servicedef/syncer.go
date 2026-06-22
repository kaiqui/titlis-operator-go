package servicedef

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"gopkg.in/yaml.v3"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/titlis/operator/internal/queue"
)

// WorkloadMatch correlaciona o service.yaml com workloads (namespaces + regex no nome).
type WorkloadMatch struct {
	Namespaces  []string `yaml:"namespaces" json:"namespaces"`
	NamePattern string   `yaml:"name_pattern" json:"name_pattern"`
}

// GitopsPath é o caminho do manifest + base branch por ambiente.
type GitopsPath struct {
	Path       string `yaml:"path" json:"path"`
	BaseBranch string `yaml:"base_branch" json:"base_branch"`
}

// ServiceYAML represents the parsed .titlis/service.yaml file.
type ServiceYAML struct {
	APIVersion string `yaml:"apiVersion"`
	Kind       string `yaml:"kind"`
	Metadata   struct {
		Name          string        `yaml:"name"`
		WorkloadMatch WorkloadMatch `yaml:"workload_match"`
	} `yaml:"metadata"`
	Spec struct {
		Team        string `yaml:"team"` // legado; preferir owner.team
		Owner       struct {
			Team string `yaml:"team"`
		} `yaml:"owner"`
		Product     string `yaml:"product"`
		Tier        string `yaml:"tier"`
		Description string `yaml:"description"`
		Workloads   []string `yaml:"workloads"`
		Gitops      struct {
			Paths map[string]GitopsPath `yaml:"paths"`
		} `yaml:"gitops"`
		Remediation  map[string]interface{}   `yaml:"remediation"`
		Integrations []queue.QueueIntegration `yaml:"integrations"`
	} `yaml:"spec"`
}

// ServiceDefinitionPayload is the data sent in the service_definition_synced event.
type ServiceDefinitionPayload struct {
	ServiceName   string                   `json:"service_name"`
	Team          string                   `json:"team"`
	Product       string                   `json:"product,omitempty"`
	Tier          string                   `json:"tier,omitempty"`
	Description   string                   `json:"description,omitempty"`
	RepoURL       string                   `json:"repo_url,omitempty"`
	Workloads     []string                 `json:"workloads"`
	RawYAML       string                   `json:"raw_yaml,omitempty"`
	Integrations  []queue.QueueIntegration `json:"integrations,omitempty"`
	WorkloadMatch *WorkloadMatch           `json:"workload_match,omitempty"`
	GitopsPaths   map[string]GitopsPath    `json:"gitops_paths,omitempty"`
	Remediation   map[string]interface{}   `json:"remediation,omitempty"`
}

// GitHubTokenProvider provides the GitHub token for the operator's tenant.
type GitHubTokenProvider interface {
	GetGitHubToken(ctx context.Context) (string, error)
}

// EventSender sends service_definition_synced events to titlis-api.
type EventSender interface {
	SendServiceDefinitionSynced(ctx context.Context, payload ServiceDefinitionPayload)
}

// Syncer detects the titlis.io/service-repo annotation on Deployments and
// syncs the .titlis/service.yaml file to titlis-api.
type Syncer struct {
	tokenProvider GitHubTokenProvider
	eventSender   EventSender
	httpClient    *http.Client
}

func NewSyncer(tokenProvider GitHubTokenProvider, eventSender EventSender) *Syncer {
	return &Syncer{
		tokenProvider: tokenProvider,
		eventSender:   eventSender,
		httpClient:    &http.Client{Timeout: 10 * time.Second},
	}
}

func (s *Syncer) Sync(ctx context.Context, repoURL, workloadName string) {
	if repoURL == "" {
		return
	}

	logger := log.FromContext(ctx).WithValues("repo_url", repoURL, "workload", workloadName)

	token, err := s.tokenProvider.GetGitHubToken(ctx)
	if err != nil {
		logger.V(1).Info("github token not available, skipping service.yaml sync", "err", err)
		return
	}
	if token == "" {
		logger.V(1).Info("github token empty, skipping service.yaml sync")
		return
	}

	rawYAML, err := fetchServiceYAML(ctx, s.httpClient, repoURL, token)
	if err != nil {
		logger.Error(err, "failed to fetch .titlis/service.yaml")
		return
	}
	if rawYAML == "" {
		logger.V(1).Info(".titlis/service.yaml not found in repo")
		return
	}

	svc, err := parseServiceYAML(rawYAML)
	if err != nil {
		logger.Error(err, "failed to parse .titlis/service.yaml")
		return
	}
	team := svc.Spec.Owner.Team
	if team == "" {
		team = svc.Spec.Team
	}
	if team == "" {
		logger.V(1).Info(".titlis/service.yaml has no team defined")
		return
	}

	serviceName := svc.Metadata.Name
	if serviceName == "" {
		serviceName = workloadName
	}

	workloads := svc.Spec.Workloads
	if len(workloads) == 0 {
		workloads = []string{workloadName}
	}

	var workloadMatch *WorkloadMatch
	if svc.Metadata.WorkloadMatch.NamePattern != "" || len(svc.Metadata.WorkloadMatch.Namespaces) > 0 {
		wm := svc.Metadata.WorkloadMatch
		workloadMatch = &wm
	}

	s.eventSender.SendServiceDefinitionSynced(ctx, ServiceDefinitionPayload{
		ServiceName:   serviceName,
		Team:          team,
		Product:       svc.Spec.Product,
		Tier:          svc.Spec.Tier,
		Description:   svc.Spec.Description,
		RepoURL:       repoURL,
		Workloads:     workloads,
		RawYAML:       rawYAML,
		Integrations:  svc.Spec.Integrations,
		WorkloadMatch: workloadMatch,
		GitopsPaths:   svc.Spec.Gitops.Paths,
		Remediation:   svc.Spec.Remediation,
	})

	logger.Info("service definition synced", "service", serviceName, "team", team)
}

func parseServiceYAML(raw string) (*ServiceYAML, error) {
	var svc ServiceYAML
	if err := yaml.Unmarshal([]byte(raw), &svc); err != nil {
		return nil, fmt.Errorf("yaml unmarshal: %w", err)
	}
	return &svc, nil
}
