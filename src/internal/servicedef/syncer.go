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

// ServiceYAML represents the parsed .titlis/service.yaml file.
type ServiceYAML struct {
	APIVersion string `yaml:"apiVersion"`
	Kind       string `yaml:"kind"`
	Metadata   struct {
		Name string `yaml:"name"`
	} `yaml:"metadata"`
	Spec struct {
		Team         string                   `yaml:"team"`
		Product      string                   `yaml:"product"`
		Tier         string                   `yaml:"tier"`
		Description  string                   `yaml:"description"`
		Workloads    []string                 `yaml:"workloads"`
		Integrations []queue.QueueIntegration `yaml:"integrations"`
	} `yaml:"spec"`
}

// ServiceDefinitionPayload is the data sent in the service_definition_synced event.
type ServiceDefinitionPayload struct {
	ServiceName  string                   `json:"service_name"`
	Team         string                   `json:"team"`
	Product      string                   `json:"product,omitempty"`
	Tier         string                   `json:"tier,omitempty"`
	Description  string                   `json:"description,omitempty"`
	RepoURL      string                   `json:"repo_url,omitempty"`
	Workloads    []string                 `json:"workloads"`
	RawYAML      string                   `json:"raw_yaml,omitempty"`
	Integrations []queue.QueueIntegration `json:"integrations,omitempty"`
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
	if svc.Spec.Team == "" {
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

	s.eventSender.SendServiceDefinitionSynced(ctx, ServiceDefinitionPayload{
		ServiceName:  serviceName,
		Team:         svc.Spec.Team,
		Product:      svc.Spec.Product,
		Tier:         svc.Spec.Tier,
		Description:  svc.Spec.Description,
		RepoURL:      repoURL,
		Workloads:    workloads,
		RawYAML:      rawYAML,
		Integrations: svc.Spec.Integrations,
	})

	logger.Info("service definition synced", "service", serviceName, "team", svc.Spec.Team)
}

func parseServiceYAML(raw string) (*ServiceYAML, error) {
	var svc ServiceYAML
	if err := yaml.Unmarshal([]byte(raw), &svc); err != nil {
		return nil, fmt.Errorf("yaml unmarshal: %w", err)
	}
	return &svc, nil
}
