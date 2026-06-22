package datadog

import (
	"fmt"

	"github.com/titlis/operator/internal/discovery"
)

// builder accumulates Datadog assets/relations for one sweep. monitorExists gates dd_slo→dd_monitor
// edges to monitors actually discovered this cycle.
type builder struct {
	assets        []discovery.Asset
	relations     []discovery.Relation
	monitorExists map[string]bool
	errors        []string
}

func newBuilder() *builder {
	return &builder{monitorExists: map[string]bool{}}
}

func (b *builder) add(a discovery.Asset) { b.assets = append(b.assets, a) }

func (b *builder) fail(resource string, err error) {
	b.errors = append(b.errors, fmt.Sprintf("%s: %v", resource, err))
}

func (b *builder) rel(src, tgt, typ string) {
	b.relations = append(b.relations, discovery.Relation{
		SourceExternalID: src, SourceProvider: providerName,
		TargetExternalID: tgt, TargetProvider: providerName, Type: typ,
	})
}
