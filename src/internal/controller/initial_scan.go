package controller

import (
	"context"
	"fmt"
	"sort"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/titlis/operator/internal/titlisapi"
)

// initialClusterScan runs once after the cache syncs and reconciles every
// Deployment in the cluster so the first scorecard cycle is complete before
// the regular watch-based loop takes over.
// After the scan it also signals the API which namespaces are excluded so
// previously-monitored workloads get soft-deleted from the platform view.
type initialClusterScan struct {
	controller *ScorecardController
	cache      cache.Cache
}

func (s *initialClusterScan) Start(ctx context.Context) error {
	logger := log.FromContext(ctx).WithName("initial-scan")

	if !s.cache.WaitForCacheSync(ctx) {
		return fmt.Errorf("cache sync timed out during initial cluster scan")
	}

	var deployList appsv1.DeploymentList
	if err := s.controller.List(ctx, &deployList); err != nil {
		logger.Error(err, "failed to list deployments for initial scan")
		return nil
	}

	total := len(deployList.Items)
	logger.Info("starting initial cluster scan", "deployments", total)

	failed := 0
	for _, deploy := range deployList.Items {
		req := ctrl.Request{
			NamespacedName: types.NamespacedName{
				Namespace: deploy.Namespace,
				Name:      deploy.Name,
			},
		}
		if _, err := s.controller.Reconcile(ctx, req); err != nil {
			logger.Error(err, "initial scan: reconcile failed",
				"namespace", deploy.Namespace,
				"name", deploy.Name)
			failed++
		}
	}

	logger.Info("initial cluster scan complete",
		"total", total,
		"failed", failed,
		"succeeded", total-failed)

	s.syncNamespaceExclusions(ctx, logger)
	return nil
}

func (s *initialClusterScan) syncNamespaceExclusions(ctx context.Context, logger interface{ Info(string, ...any) }) {
	if s.controller.TitlisAPI == nil {
		return
	}
	excluded := s.controller.Exclusions.ExcludedNamespaces()
	sorted := make([]string, len(excluded))
	copy(sorted, excluded)
	sort.Strings(sorted)

	payload := titlisapi.NamespaceExclusionsSyncPayload{
		Cluster:            s.controller.Settings.KubernetesClusterName,
		ExcludedNamespaces: sorted,
	}
	s.controller.TitlisAPI.SendNamespaceExclusionsSync(ctx, payload)
	logger.Info("namespace exclusions synced with api", "excluded_count", len(sorted))
}

// NeedLeaderElection garante que o scan só roda no pod líder.
func (s *initialClusterScan) NeedLeaderElection() bool { return true }
