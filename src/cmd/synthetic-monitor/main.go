package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/kelseyhightower/envconfig"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"github.com/titlis/operator/internal/config"
	ddatadog "github.com/titlis/operator/internal/observability/datadog"
	"github.com/titlis/operator/internal/synthetic"
)

func main() {
	var cfg config.Settings
	if err := envconfig.Process("", &cfg); err != nil {
		fmt.Fprintf(os.Stderr, "config error: %v\n", err)
		os.Exit(1)
	}

	logOpts := zap.Options{Development: cfg.LogLevel == "debug" || cfg.LogLevel == "DEBUG"}
	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&logOpts)))
	logger := ctrl.Log.WithName("synthetic-monitor")

	siteChecks, jsonChecks := synthetic.LoadChecks(&cfg)

	if len(siteChecks)+len(jsonChecks) == 0 {
		logger.Info("nenhum check configurado — monitor sintético ocioso")
		os.Exit(0)
	}

	logger.Info("Synthetic Monitor iniciado",
		"site_checks", len(siteChecks),
		"json_checks", len(jsonChecks),
	)

	ddMetrics := ddatadog.NewMetricsClient(&cfg)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	synthetic.Run(ctx, siteChecks, jsonChecks, ddMetrics)
	logger.Info("Synthetic Monitor encerrado")
}
