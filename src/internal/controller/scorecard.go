package controller

import (
	"context"

	appsv1 "k8s.io/api/apps/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/titlis/operator/internal/config"
	"github.com/titlis/operator/internal/scorecard"
	"github.com/titlis/operator/internal/titlisapi"
)

type ScorecardController struct {
	client.Client
	Exclusions *scorecard.ExclusionFilter
	TitlisAPI  *titlisapi.Client
	Settings   *config.Settings
}

func (r *ScorecardController) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithName("scorecard").WithValues(
		"namespace", req.Namespace, "name", req.Name,
	)

	if r.Exclusions.IsExcluded(req.Namespace) {
		logger.V(1).Info("namespace excluded, skipping")
		return ctrl.Result{}, nil
	}

	var deploy appsv1.Deployment
	if err := r.Get(ctx, req.NamespacedName, &deploy); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if r.TitlisAPI == nil {
		logger.V(1).Info("titlis-api not configured, skipping evaluation")
		return ctrl.Result{RequeueAfter: reconcileInterval(r.Settings)}, nil
	}

	hpa := scorecard.LookupHPA(ctx, req.Namespace, req.Name, r.Client)
	hasNetPolicy := scorecard.HasNetworkPolicy(ctx, req.Namespace, r.Client)
	snap := scorecard.ExtractSnapshot(&deploy, hpa, hasNetPolicy,
		r.Settings.KubernetesClusterName, "kubernetes")

	logger.V(1).Info("snapshot extracted, queuing evaluation",
		"uid", snap.UID,
		"replicas", snap.Replicas,
		"has_hpa", snap.HasHPA,
		"has_net_policy", snap.HasNetworkPolicy,
		"cluster", snap.Cluster,
	)
	go r.TitlisAPI.EvaluateWorkload(context.Background(), snap)

	return ctrl.Result{RequeueAfter: reconcileInterval(r.Settings)}, nil
}

func (r *ScorecardController) SetupWithManager(mgr ctrl.Manager) error {
	if err := mgr.Add(&initialClusterScan{
		controller: r,
		cache:      mgr.GetCache(),
	}); err != nil {
		return err
	}
	return ctrl.NewControllerManagedBy(mgr).
		For(&appsv1.Deployment{}).
		Complete(r)
}
