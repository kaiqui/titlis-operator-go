// Package discovery is the operator's Discovery Engine: it enumerates the organization's assets
// from multiple sources into one normalized graph and ships it to titlis-api. It discovers
// ("what exists") — it never scores ("what is good/bad"); scoring lives in titlis-scoreops (regra 14).
//
// # Contrato de um Provider
//
// Uma fonte de descoberta implementa [Provider]:
//
//	Name()    string                 // estável: "kubernetes", "datadog", "dynatrace", "otel"
//	Enabled() bool                   // false quando a fonte está desligada nesta instância
//	Discover(ctx) (AssetSubgraph, error)
//
// Adicionar uma fonte nova é drop-in: implemente [Provider], registre no [Registry] (em main) e
// pronto — nada no [DiscoveryRunner] nem no contrato HTTP muda. Correlações cross-provider entram
// como um [Correlator] via [Registry.WithCorrelators].
//
// # Convenções (obrigatórias para a ingestão idempotente da titlis-api)
//
//   - Asset.Provider deve ser igual a Provider.Name().
//   - Asset.ExternalID é a chave natural por provider e deve ser estável entre sweeps. Prefixe por
//     tipo quando o id cru puder colidir (ex.: "service:orders", "monitor:123", "metric:jvm.heap").
//     Para K8s use o UID do objeto.
//   - Asset.Kind em snake_case minúsculo ("deployment", "dd_monitor", "otel_service").
//   - Asset.Name sempre preenchido (legível). Asset.Tags = labels/tags; Asset.Attributes = shape
//     específico do tipo (livre).
//   - Relation referencia (provider, externalId) de origem/destino — a titlis-api resolve para os ids
//     surrogados na ingestão. Reenvie todas as relações a cada sweep (o soft-delete depende disso).
//
// # Status e degradação graciosa
//
// Discover nunca deve entrar em pânico e, em geral, retorna err == nil — falhas são reportadas em
// AssetSubgraph.Status:
//
//   - [StatusOK]            tudo coletado
//   - [StatusPartial]       coletou parte (alguns endpoints falharam); detalhe em Status.Error
//   - [StatusError]         não coletou nada por erro; detalhe em Status.Error
//   - [StatusNotConfigured] a fonte não está conectada (ex.: sem credenciais). NÃO é falha e NÃO é
//     cobertura zero — o downstream marca as dimensões dependentes como N/A.
//
// Um provider sem credencial/endpoint deve retornar StatusNotConfigured com zero assets, sem logar
// segredo e sem erro (regra 15).
package discovery
