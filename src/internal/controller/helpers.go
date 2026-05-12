package controller

import (
	"time"

	"github.com/titlis/operator/internal/config"
)

func reconcileInterval(cfg *config.Settings) time.Duration {
	if cfg == nil || cfg.ReconcileIntervalSeconds == 0 {
		return 5 * time.Minute
	}
	return time.Duration(cfg.ReconcileIntervalSeconds) * time.Second
}
