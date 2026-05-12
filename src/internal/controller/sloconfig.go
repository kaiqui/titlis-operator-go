package controller

import (
	"context"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/titlis/operator/api/v1alpha1"
	"github.com/titlis/operator/internal/config"
	"github.com/titlis/operator/internal/notification"
	"github.com/titlis/operator/internal/slo"
	"github.com/titlis/operator/internal/titlisapi"
)

type SLOConfigController struct {
	client.Client
	SloSvc    *slo.Service
	Notifier  notification.Notifier
	TitlisAPI *titlisapi.Client
	Settings  *config.Settings
}

func (r *SLOConfigController) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithName("sloconfig").WithValues(
		"name", req.Name, "namespace", req.Namespace,
	)

	var sloConfig v1alpha1.SLOConfig
	if err := r.Get(ctx, req.NamespacedName, &sloConfig); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Validate required fields
	if sloConfig.Spec.Service == "" {
		logger.Info("validation failed", "reason", "spec.service is required")
		return r.setError(ctx, &sloConfig, "spec.service é obrigatório")
	}
	if sloConfig.Spec.Warning != nil && *sloConfig.Spec.Warning <= sloConfig.Spec.Target {
		logger.Info("validation failed", "reason", "warning must be greater than target",
			"target", sloConfig.Spec.Target, "warning", *sloConfig.Spec.Warning)
		return r.setError(ctx, &sloConfig, "warning deve ser maior que target")
	}

	newStatus, err := r.SloSvc.Reconcile(ctx, &sloConfig)
	if err != nil {
		logger.Error(err, "slo service reconcile failed", "service", sloConfig.Spec.Service)
		errMsg := err.Error()
		newStatus = &v1alpha1.SLOConfigStatus{
			State: "error",
			Error: &errMsg,
		}
	}

	patch := client.MergeFrom(sloConfig.DeepCopy())
	sloConfig.Status = *newStatus
	if err := r.Status().Patch(ctx, &sloConfig, patch); err != nil {
		logger.Error(err, "failed to patch SLOConfig status")
		return ctrl.Result{}, err
	}

	if newStatus.State == "error" && newStatus.Error != nil {
		logger.Info("SLOConfig reconciled with error",
			"service", sloConfig.Spec.Service, "error", *newStatus.Error)
	} else {
		sloID := ""
		if newStatus.SLOID != nil {
			sloID = *newStatus.SLOID
		}
		logger.Info("SLOConfig reconciled",
			"service", sloConfig.Spec.Service, "state", newStatus.State, "slo_id", sloID)
	}

	go r.notifyAndSend(context.Background(), &sloConfig, newStatus)

	return ctrl.Result{RequeueAfter: reconcileInterval(r.Settings)}, nil
}

func (r *SLOConfigController) setError(ctx context.Context,
	sloConfig *v1alpha1.SLOConfig, errMsg string) (ctrl.Result, error) {

	patch := client.MergeFrom(sloConfig.DeepCopy())
	sloConfig.Status.State = "error"
	sloConfig.Status.Error = &errMsg
	_ = r.Status().Patch(ctx, sloConfig, patch)
	return ctrl.Result{}, nil
}

func (r *SLOConfigController) notifyAndSend(ctx context.Context,
	sloConfig *v1alpha1.SLOConfig, status *v1alpha1.SLOConfigStatus) {

	logger := log.FromContext(ctx)

	if r.Notifier != nil {
		sev := notification.SeverityInfo
		msg := "SLO reconciliado: " + sloConfig.Spec.Service
		if status.State == "error" && status.Error != nil {
			sev = notification.SeverityError
			msg = "Erro ao reconciliar SLO " + sloConfig.Spec.Service + ": " + *status.Error
		}
		if err := r.Notifier.Send(ctx, "", "SLO Sync", msg, sev); err != nil {
			logger.Error(err, "failed to send SLO notification")
		}
	}

	if r.TitlisAPI != nil && status.SLOID != nil {
		r.TitlisAPI.SendSLOReconciled(ctx, titlisapi.SLOReconciledPayload{
			SLOID:   *status.SLOID,
			Service: sloConfig.Spec.Service,
			Target:  sloConfig.Spec.Target,
			State:   status.State,
		})
	}
}

func (r *SLOConfigController) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.SLOConfig{}).
		Complete(r)
}
