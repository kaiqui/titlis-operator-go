package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/kelseyhightower/envconfig"
	appsv1 "k8s.io/api/apps/v1"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"k8s.io/apimachinery/pkg/runtime"

	"github.com/titlis/operator/internal/castai"
	"github.com/titlis/operator/internal/config"
	ddatadog "github.com/titlis/operator/internal/observability/datadog"
)

func main() {
	var cfg config.Settings
	if err := envconfig.Process("", &cfg); err != nil {
		fmt.Fprintf(os.Stderr, "config error: %v\n", err)
		os.Exit(1)
	}

	logOpts := zap.Options{Development: cfg.LogLevel == "debug" || cfg.LogLevel == "DEBUG"}
	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&logOpts)))
	logger := ctrl.Log.WithName("castai-monitor")

	if !cfg.CastAIEnabled {
		logger.Info("ENABLE_CASTAI_MONITOR=false — monitor encerrado sem iniciar")
		os.Exit(0)
	}

	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		fmt.Fprintf(os.Stderr, "scheme error: %v\n", err)
		os.Exit(1)
	}
	if err := appsv1.AddToScheme(scheme); err != nil {
		fmt.Fprintf(os.Stderr, "scheme error: %v\n", err)
		os.Exit(1)
	}

	k8sCfg, err := ctrl.GetConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "kubeconfig error: %v\n", err)
		os.Exit(1)
	}
	k8sClient, err := client.New(k8sCfg, client.Options{Scheme: scheme})
	if err != nil {
		fmt.Fprintf(os.Stderr, "k8s client error: %v\n", err)
		os.Exit(1)
	}

	ddMetrics := ddatadog.NewMetricsClient(&cfg)
	runner := castai.NewRunner(k8sClient, ddMetrics, &cfg)

	logger.Info("CAST AI Monitor iniciado",
		"cluster", cfg.CastAIClusterName,
		"namespace", cfg.CastAIMonitorNamespace,
		"interval_seconds", cfg.CastAIMonitorInterval,
	)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	if err := runner.Start(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "monitor error: %v\n", err)
		os.Exit(1)
	}
	logger.Info("CAST AI Monitor encerrado")
}
