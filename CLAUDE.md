# CLAUDE.md — titlis-operator-go

> **Este é o operator oficial da plataforma Titlis.** O diretório `titlis-operator/` contém
> um operator legado em Python (Kopf) que não é mais o operator canônico.
>
> Após toda alteração: `make build` e `make test` devem passar.

---

## 1. Visão Geral

O **titlis-operator-go** é o Kubernetes Operator oficial escrito em Go (controller-runtime).
Ele é o único ator que interage diretamente com a API do Kubernetes.

Responsabilidades:
- **Extração de snapshot** — coleta estado de cada Deployment (recursos, HPA, NetworkPolicy,
  labels, annotations, réplicas) e envia para `titlis-scoreops` avaliar
- **SLO sync** — assiste CRDs `SLOConfig` e sincroniza com Datadog
- **SLO pending** — polling de mudanças de SLO propostas pelo titlis-ai
- **Synthetic monitor** — monitora URLs externas e envia métricas ao Datadog
- **CastAI monitor** — monitora agente CastAI em execução no cluster

O operator **não calcula scores** — isso é responsabilidade do `titlis-scoreops`.
O operator **não escreve CRDs `AppScorecard`** — a fase 5 da migração removeu isso.
O operator **não abre PRs** — isso é responsabilidade do `titlis-ai`.

---

## 2. Stack

| Categoria | Tecnologia | Versão |
|---|---|---|
| Linguagem | Go | 1.22+ |
| Framework K8s | controller-runtime | 0.19.x |
| Config | kelseyhightower/envconfig | 1.4.0 |
| Kubernetes client | client-go | v0.31.x |
| Build | Makefile + ko / Docker | — |

---

## 3. Estrutura do Projeto

```
titlis-operator-go/
├── src/
│   ├── cmd/operator/main.go        # Entrypoint: manager, controllers, runnables
│   └── internal/
│       ├── castai/                 # Monitor do agente CastAI
│       ├── config/
│       │   └── settings.go         # Struct Settings (envconfig)
│       ├── controller/
│       │   ├── scorecard.go        # ScorecardController — watch Deployments
│       │   ├── initial_scan.go     # Scan inicial de todos os Deployments
│       │   ├── slo.go              # SLOConfigController — watch SLOConfig CRDs
│       │   └── slo_pending.go      # SLOPendingRunner — polling de mudanças
│       ├── k8s/                    # Helpers K8s (sem AppScorecardWriter — removido na fase 5)
│       ├── model/                  # Tipos compartilhados (ScorecardConfig, WorkloadSnapshot)
│       ├── notification/           # Interfaces de notificação
│       │   └── slack/              # Implementação Slack
│       ├── observability/
│       │   └── datadog/            # Clientes Datadog (SLO + Metrics)
│       ├── scorecard/
│       │   ├── config.go           # LoadConfig + ExclusionFilter
│       │   └── extractor.go        # ExtractSnapshot, LookupHPA, HasNetworkPolicy
│       ├── slo/                    # Lógica de sync de SLOs com Datadog
│       ├── synthetic/              # Synthetic monitor HTTP
│       └── titlisapi/
│           └── client.go           # Cliente HTTP para titlis-api
├── charts/titlis-operator/         # Helm chart (sem CRD appscorecards — removido na fase 5)
│   └── crds/
│       ├── appremediations.titlis.io.yaml
│       └── sloconfigs.titlis.io.yaml
└── config/
    └── scorecard-config.yaml       # Namespaces excluídos (apenas exclusões)
```

---

## 4. Fluxo Principal — ScorecardController

```
Deployment criado/atualizado/deletado
  ↓
ScorecardController.Reconcile(ctx, req)
  ├── ExclusionFilter.IsExcluded(namespace) → ignora se excluído
  ├── r.Get(deploy) → ignora se não encontrado (deletado)
  ├── scorecard.LookupHPA(ctx, ns, name, client)
  ├── scorecard.HasNetworkPolicy(ctx, ns, client)
  ├── scorecard.ExtractSnapshot(&deploy, hpa, hasNetPolicy, cluster, "kubernetes")
  └── go r.TitlisAPI.EvaluateWorkload(ctx, snap)   ← fire-and-forget
      ↓
      POST /v1/operator/scoring/evaluate → titlis-api → titlis-scoreops
      (scoreops calcula, persiste no banco, notifica titlis-api via scorecard_evaluated)
```

**Reconcile interval:** configurado por `RECONCILE_INTERVAL_SECONDS` (padrão: 300s).

**Fire-and-forget:** falhas de envio são logadas mas não causam requeue — o próximo ciclo
de reconciliação trata isso naturalmente.

### initialClusterScan

Roda uma única vez após o cache K8s sincronizar (implementa `NeedLeaderElection() = true`):
1. Lista todos os Deployments do cluster
2. Chama `Reconcile` para cada um — garante que o primeiro scorecard cycle está completo
3. Chama `syncNamespaceExclusions` — sincroniza namespaces excluídos com titlis-api

---

## 5. ExclusionFilter

`scorecard/config.go` define `ExclusionFilter` — substituto leve do `scorecard.Service` removido.

```go
filter := scorecard.NewExclusionFilter(scorecard.LoadConfig(path))
filter.IsExcluded("kube-system")   // true
filter.ExcludedNamespaces()        // []string{...}
```

**Namespaces excluídos por padrão** (hardcoded em `DefaultExcludedNamespaces`):
```
kube-system, kube-public, kube-node-lease, datadog, titlis-operator, titlis-system
```

Namespaces adicionais são lidos de `config/scorecard-config.yaml`:
```yaml
excluded_namespaces:
  - monitoring
  - logging
```

---

## 6. titlisapi.Client — Métodos Disponíveis

```go
// Envia snapshot para avaliação (fire-and-forget via goroutine)
client.EvaluateWorkload(ctx, snap model.WorkloadSnapshot)

// Envia evento scorecard_evaluated diretamente (legacy, ainda usado em testes)
client.SendScorecardEvaluated(ctx, payload ScorecardEvaluatedPayload)

// Sincroniza namespaces excluídos
client.SendNamespaceExclusionsSync(ctx, payload NamespaceExclusionsSyncPayload)

// Polling de mudanças de SLO
client.GetPendingSLOChanges(ctx, clusterName string) ([]PendingSLOChange, error)
client.MarkSLOChangeApplied(ctx, changeID string) error
client.MarkSLOChangeFailed(ctx, changeID, reason string) error

// Outros eventos
client.SendSLOReconciled(ctx, payload SLOReconciledPayload)
client.SendNotificationSent(ctx, payload NotificationSentPayload)
```

**Auth:** header `X-Api-Key: <TITLIS_API_API_KEY>` em todas as requisições.

---

## 7. SLOConfigController

Assiste CRDs `SLOConfig` (GVK: `titlis.io/v1alpha1/SLOConfig`).

Fluxo de reconciliação (3 paths):
- **Path A (Create)** — SLO não existe no Datadog → cria via API DD
- **Path B (Update)** — SLO existe mas diverge (target/timeframe) → atualiza
- **Path C (Delete)** — SLO marcado para deleção → deleta no Datadog

`SLOPendingRunner` faz polling em `GET /v1/operator/pending-slo-changes` a cada
`SLO_PENDING_POLL_SECONDS` (padrão: 30s) e aplica mudanças propostas pelo titlis-ai.

---

## 8. Variáveis de Ambiente

```bash
# Kubernetes
KUBERNETES_NAMESPACE=titlis-system
KUBERNETES_CLUSTER_NAME=prod-k8s

# Controllers
ENABLE_SCORECARD_CONTROLLER=true
ENABLE_SLO_CONTROLLER=true

# Reconcile
RECONCILE_INTERVAL_SECONDS=300
DEBOUNCE_SECONDS=30

# Leader election
ENABLE_LEADER_ELECTION=true
LEADER_ELECTION_NAMESPACE=titlis

# titlis-api
TITLIS_API_ENABLED=true
TITLIS_API_HOST=titlis-api.titlis-system.svc.cluster.local
TITLIS_API_HTTP_PORT=8080
TITLIS_API_API_KEY=tk_...

# Datadog (para SLO controller)
DD_API_KEY=...
DD_APP_KEY=...
DD_SITE=datadoghq.com

# Slack (opcional)
SLACK_ENABLED=false
SLACK_WEBHOOK_URL=https://hooks.slack.com/...

# Scorecard config
SCORECARD_CONFIG_PATH=config/scorecard-config.yaml

# Synthetic monitor (opcional)
ENABLE_SYNTHETIC_MONITOR=false
SYNTHETIC_CHECKS_CONFIG_PATH=config/synthetic-checks.yaml

# CastAI (opcional)
ENABLE_CASTAI_MONITOR=false
CASTAI_API_KEY=...
CASTAI_CLUSTER_ID=...

# Log
LOG_LEVEL=info
```

---

## 9. Comandos de Desenvolvimento

```bash
# Build
make build       # go build ./...
make test        # go test ./...
make lint        # golangci-lint run

# Rodar localmente (requer kubeconfig configurado)
make run

# Docker
make docker-build IMAGE=kailima/titlis-operator-go:latest
```

---

## 10. CRDs Instalados

Os CRDs são aplicados via `kubectl apply` durante o deploy:

| CRD | Short name | Gerenciado por |
|---|---|---|
| `appremediations.titlis.io` | `ar` | titlis-ai cria, operator apenas lê |
| `sloconfigs.titlis.io` | `sloc` | SLOConfigController reconcilia |

> **`appscorecards.titlis.io` foi removido na fase 5** — o operator não mais escreve CRDs
> de scorecard. Os scores existem apenas no PostgreSQL via titlis-scoreops → titlis-api.

---

## 11. O Que Não Fazer

- **Nunca** acesse a API do Kubernetes a partir do titlis-ai ou titlis-scoreops — o operator
  é o único ator K8s
- **Nunca** calcule scores dentro do operator — envie o snapshot para titlis-scoreops
- **Nunca** abra PRs no GitHub — isso é responsabilidade exclusiva do titlis-ai
- **Nunca** remova um Deployment do banco via HTTP DELETE — use eventos para soft-delete
- **Nunca** bloqueie o goroutine do Reconcile com I/O síncrono para titlis-api — use `go`
