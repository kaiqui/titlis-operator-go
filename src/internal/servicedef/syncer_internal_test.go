package servicedef

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseServiceYAML_Integrations(t *testing.T) {
	raw := `
apiVersion: titlis.io/v1
kind: Service
metadata:
  name: orders-api
spec:
  team: plataforma
  integrations:
    - type: gcp_pubsub
      match: display_name
      queues:
        - "orders-*"
        - "orders.events"
`
	svc, err := parseServiceYAML(raw)
	assert.NoError(t, err)
	assert.Equal(t, "orders-api", svc.Metadata.Name)
	assert.Equal(t, "plataforma", svc.Spec.Team)
	assert.Len(t, svc.Spec.Integrations, 1)
	assert.Equal(t, "gcp_pubsub", svc.Spec.Integrations[0].Type)
	assert.Equal(t, "display_name", svc.Spec.Integrations[0].Match)
	assert.Equal(t, []string{"orders-*", "orders.events"}, svc.Spec.Integrations[0].Queues)
}

func TestParseServiceYAML_NoIntegrations(t *testing.T) {
	raw := `
metadata:
  name: legacy
spec:
  team: core
`
	svc, err := parseServiceYAML(raw)
	assert.NoError(t, err)
	assert.Empty(t, svc.Spec.Integrations)
}
