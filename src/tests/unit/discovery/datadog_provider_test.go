package discovery_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/titlis/operator/internal/discovery"
	dddiscovery "github.com/titlis/operator/internal/discovery/datadog"
	"github.com/titlis/operator/internal/queue"
)

type fakeCreds struct {
	creds *queue.DDCredentials
	err   error
}

func (f fakeCreds) GetDatadogConfig(_ context.Context) (*queue.DDCredentials, error) {
	return f.creds, f.err
}

func TestDatadogProvider_NotConfiguredWhenNoCreds(t *testing.T) {
	p := dddiscovery.New(fakeCreds{creds: nil}, dddiscovery.Options{Enabled: true})
	sub, err := p.Discover(context.Background())

	assert.NoError(t, err)
	assert.Empty(t, sub.Assets)
	assert.Equal(t, discovery.StatusNotConfigured, sub.Status.Status)
	assert.Empty(t, sub.Status.Error)
}

func TestDatadogProvider_NotConfiguredWhenBlankKeys(t *testing.T) {
	p := dddiscovery.New(fakeCreds{creds: &queue.DDCredentials{APIKey: "", AppKey: ""}}, dddiscovery.Options{Enabled: true})
	sub, err := p.Discover(context.Background())

	assert.NoError(t, err)
	assert.Equal(t, discovery.StatusNotConfigured, sub.Status.Status)
}

func TestDatadogProvider_ErrorFromCredsFetcher(t *testing.T) {
	p := dddiscovery.New(fakeCreds{err: errors.New("api-down")}, dddiscovery.Options{Enabled: true})
	sub, err := p.Discover(context.Background())

	assert.NoError(t, err)
	assert.Equal(t, discovery.StatusError, sub.Status.Status)
	assert.Equal(t, "api-down", sub.Status.Error)
}

func TestDatadogProvider_EnabledFlag(t *testing.T) {
	assert.False(t, dddiscovery.New(fakeCreds{}, dddiscovery.Options{Enabled: false}).Enabled())
	assert.True(t, dddiscovery.New(fakeCreds{}, dddiscovery.Options{Enabled: true}).Enabled())
	assert.Equal(t, "datadog", dddiscovery.New(fakeCreds{}, dddiscovery.Options{}).Name())
}
