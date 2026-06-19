package servicedef

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- parseGitHubRepo ---

func TestParseGitHubRepo_HTTPSPrefix(t *testing.T) {
	owner, repo, err := parseGitHubRepo("https://github.com/myorg/myrepo")
	require.NoError(t, err)
	assert.Equal(t, "myorg", owner)
	assert.Equal(t, "myrepo", repo)
}

func TestParseGitHubRepo_BareFormat(t *testing.T) {
	owner, repo, err := parseGitHubRepo("github.com/myorg/myrepo")
	require.NoError(t, err)
	assert.Equal(t, "myorg", owner)
	assert.Equal(t, "myrepo", repo)
}

func TestParseGitHubRepo_DotGitSuffix(t *testing.T) {
	owner, repo, err := parseGitHubRepo("https://github.com/myorg/myrepo.git")
	require.NoError(t, err)
	assert.Equal(t, "myorg", owner)
	assert.Equal(t, "myrepo", repo)
}

func TestParseGitHubRepo_InvalidURL(t *testing.T) {
	_, _, err := parseGitHubRepo("not-a-valid-url")
	assert.Error(t, err)
}

func TestParseGitHubRepo_MissingRepo(t *testing.T) {
	_, _, err := parseGitHubRepo("github.com/myorg/")
	assert.Error(t, err)
}

// --- parseServiceYAML ---

func TestParseServiceYAML_ValidYAML(t *testing.T) {
	raw := `
apiVersion: titlis.io/v1
kind: ServiceDefinition
metadata:
  name: my-service
spec:
  team: platform
  tier: critical
  description: My service description
  workloads:
    - my-deploy
    - my-worker
`
	svc, err := parseServiceYAML(raw)
	require.NoError(t, err)
	assert.Equal(t, "my-service", svc.Metadata.Name)
	assert.Equal(t, "platform", svc.Spec.Team)
	assert.Equal(t, "critical", svc.Spec.Tier)
	assert.Equal(t, []string{"my-deploy", "my-worker"}, svc.Spec.Workloads)
}

func TestParseServiceYAML_InvalidYAML(t *testing.T) {
	_, err := parseServiceYAML("key: {unclosed")
	assert.Error(t, err)
}

func TestParseServiceYAML_EmptyTeam(t *testing.T) {
	raw := `metadata:
  name: no-team-svc
spec:
  team: ""`
	svc, err := parseServiceYAML(raw)
	require.NoError(t, err)
	assert.Empty(t, svc.Spec.Team)
}

// --- Syncer.Sync ---

type stubTokenProvider struct {
	token string
	err   error
}

func (s *stubTokenProvider) GetGitHubToken(_ context.Context) (string, error) {
	return s.token, s.err
}

type captureEventSender struct {
	received []ServiceDefinitionPayload
}

func (c *captureEventSender) SendServiceDefinitionSynced(_ context.Context, p ServiceDefinitionPayload) {
	c.received = append(c.received, p)
}

func TestSync_EmptyRepoURL_Skips(t *testing.T) {
	sender := &captureEventSender{}
	s := NewSyncer(&stubTokenProvider{token: "tk_test"}, sender)

	s.Sync(context.Background(), "", "my-deploy")

	assert.Empty(t, sender.received)
}

func TestSync_TokenError_Skips(t *testing.T) {
	sender := &captureEventSender{}
	s := NewSyncer(&stubTokenProvider{err: assert.AnError}, sender)

	s.Sync(context.Background(), "github.com/org/repo", "my-deploy")

	assert.Empty(t, sender.received)
}

func TestSync_EmptyToken_Skips(t *testing.T) {
	sender := &captureEventSender{}
	s := NewSyncer(&stubTokenProvider{token: ""}, sender)

	s.Sync(context.Background(), "github.com/org/repo", "my-deploy")

	assert.Empty(t, sender.received)
}

func TestSync_FileNotFound_Skips(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	sender := &captureEventSender{}
	s := &Syncer{
		tokenProvider: &stubTokenProvider{token: "tk_test"},
		eventSender:   sender,
		httpClient:    srv.Client(),
	}
	// Use the test server URL as the "github" host by overriding fetchServiceYAML via httpClient
	// We pass a repoURL that would hit a real github URL, but we redirect via transport below.
	s.httpClient = newRedirectClient(srv.URL)

	s.Sync(context.Background(), "github.com/org/repo", "my-deploy")

	assert.Empty(t, sender.received)
}

func TestSync_ValidServiceYAML_SendsEvent(t *testing.T) {
	yaml := `
metadata:
  name: platform-svc
spec:
  team: platform
  tier: critical
  workloads:
    - platform-deploy
`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(yaml)) //nolint:errcheck
	}))
	defer srv.Close()

	sender := &captureEventSender{}
	s := &Syncer{
		tokenProvider: &stubTokenProvider{token: "tk_test"},
		eventSender:   sender,
		httpClient:    newRedirectClient(srv.URL),
	}

	s.Sync(context.Background(), "github.com/org/repo", "my-deploy")

	require.Len(t, sender.received, 1)
	assert.Equal(t, "platform-svc", sender.received[0].ServiceName)
	assert.Equal(t, "platform", sender.received[0].Team)
	assert.Equal(t, []string{"platform-deploy"}, sender.received[0].Workloads)
}

func TestSync_NoServiceName_FallsBackToWorkloadName(t *testing.T) {
	yaml := `
spec:
  team: platform
`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(yaml)) //nolint:errcheck
	}))
	defer srv.Close()

	sender := &captureEventSender{}
	s := &Syncer{
		tokenProvider: &stubTokenProvider{token: "tk_test"},
		eventSender:   sender,
		httpClient:    newRedirectClient(srv.URL),
	}

	s.Sync(context.Background(), "github.com/org/repo", "fallback-deploy")

	require.Len(t, sender.received, 1)
	assert.Equal(t, "fallback-deploy", sender.received[0].ServiceName)
	assert.Equal(t, []string{"fallback-deploy"}, sender.received[0].Workloads)
}

func TestSync_NoTeam_Skips(t *testing.T) {
	yaml := `
metadata:
  name: no-team
spec:
  team: ""
`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(yaml)) //nolint:errcheck
	}))
	defer srv.Close()

	sender := &captureEventSender{}
	s := &Syncer{
		tokenProvider: &stubTokenProvider{token: "tk_test"},
		eventSender:   sender,
		httpClient:    newRedirectClient(srv.URL),
	}

	s.Sync(context.Background(), "github.com/org/repo", "my-deploy")

	assert.Empty(t, sender.received)
}

// newRedirectClient returns an http.Client that rewrites the host of every request
// to point to baseURL (the test server), keeping path and query intact.
func newRedirectClient(baseURL string) *http.Client {
	return &http.Client{
		Transport: &hostRewriteTransport{base: baseURL},
	}
}

type hostRewriteTransport struct {
	base string
}

func (t *hostRewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	clone.URL.Scheme = "http"
	// Extract host from base URL (strip scheme)
	host := t.base
	if len(host) > 7 && host[:7] == "http://" {
		host = host[7:]
	}
	clone.URL.Host = host
	return http.DefaultTransport.RoundTrip(clone)
}
