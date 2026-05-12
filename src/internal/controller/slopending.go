package controller

import (
	"context"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/titlis/operator/api/v1alpha1"
	"github.com/titlis/operator/internal/titlisapi"
)

// SLOPendingRunner polls titlis-api for pending SLO changes and applies them
// to the SLOConfig CRD. controller-runtime then detects the update and triggers
// SLOConfigController.Reconcile via Path A (fast path, slo_id preserved).
type SLOPendingRunner struct {
	TitlisAPI *titlisapi.Client
	K8s       client.Client
	Interval  time.Duration
	Log       logr.Logger
}

func (r *SLOPendingRunner) Start(ctx context.Context) error {
	ticker := time.NewTicker(r.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			r.process(ctx)
		}
	}
}

func (r *SLOPendingRunner) process(ctx context.Context) {
	changes, err := r.TitlisAPI.GetPendingSLOChanges(ctx)
	if err != nil {
		r.Log.Error(err, "failed to get pending SLO changes")
		return
	}

	if len(changes) == 0 {
		r.Log.V(1).Info("no pending SLO changes")
		return
	}

	r.Log.Info("processing pending SLO changes", "count", len(changes))
	for _, ch := range changes {
		if err := r.applyChange(ctx, ch); err != nil {
			r.Log.Error(err, "failed to apply SLO change",
				"id", ch.ID, "slo_config", ch.SLOConfigName, "field", ch.Field)
			_ = r.TitlisAPI.ConfirmSLOChangeFailed(ctx, ch.ID, err.Error())
		} else {
			r.Log.Info("SLO change applied",
				"id", ch.ID, "slo_config", ch.SLOConfigName, "field", ch.Field,
				"old_value", ch.OldValue, "new_value", ch.NewValue)
			_ = r.TitlisAPI.ConfirmSLOChangeApplied(ctx, ch.ID)
		}
	}
}

func (r *SLOPendingRunner) applyChange(ctx context.Context, ch titlisapi.SLOPendingChange) error {
	var sloConfig v1alpha1.SLOConfig
	if err := r.K8s.Get(ctx, types.NamespacedName{
		Namespace: ch.Namespace,
		Name:      ch.SLOConfigName,
	}, &sloConfig); err != nil {
		return err
	}

	patch := client.MergeFrom(sloConfig.DeepCopy())

	switch ch.Field {
	case "target":
		var val float64
		if _, err := fmt.Sscanf(ch.NewValue, "%f", &val); err != nil {
			return fmt.Errorf("invalid target value %q: %w", ch.NewValue, err)
		}
		sloConfig.Spec.Target = val
	case "warning":
		var val float64
		if _, err := fmt.Sscanf(ch.NewValue, "%f", &val); err != nil {
			return fmt.Errorf("invalid warning value %q: %w", ch.NewValue, err)
		}
		sloConfig.Spec.Warning = &val
	case "timeframe":
		sloConfig.Spec.Timeframe = ch.NewValue
	default:
		return fmt.Errorf("unsupported field %q", ch.Field)
	}

	return r.K8s.Patch(ctx, &sloConfig, patch)
}
