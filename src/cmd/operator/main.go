package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"go.uber.org/zap/zapcore"
	"github.com/kelseyhightower/envconfig"
	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	networkingv1 "k8s.io/api/networking/v1"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	ctrlzap "sigs.k8s.io/controller-runtime/pkg/log/zap"

	"github.com/titlis/operator/api/v1alpha1"
	"github.com/titlis/operator/internal/cluster"
	"github.com/titlis/operator/internal/config"
	"github.com/titlis/operator/internal/controller"
	"github.com/titlis/operator/internal/notification"
	slacknotifier "github.com/titlis/operator/internal/notification/slack"
	ddatadog "github.com/titlis/operator/internal/observability/datadog"
	"github.com/titlis/operator/internal/scorecard"
	"github.com/titlis/operator/internal/slo"
	"github.com/titlis/operator/internal/synthetic"
	"github.com/titlis/operator/internal/titlisapi"

	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func main() {
	var cfg config.Settings
	if err := envconfig.Process("", &cfg); err != nil {
		fmt.Fprintf(os.Stderr, "config error: %v\n", err)
		os.Exit(1)
	}

	ctrl.SetLogger(ctrlzap.New(func(o *ctrlzap.Options) {
		o.Development = false
		if strings.EqualFold(cfg.LogLevel, "debug") {
			lvl := zapcore.DebugLevel
			o.Level = &lvl
		}
	}))
	setupLog := ctrl.Log.WithName("setup")

	scheme := runtime.NewScheme()
	must(clientgoscheme.AddToScheme(scheme))
	must(appsv1.AddToScheme(scheme))
	must(autoscalingv2.AddToScheme(scheme))
	must(networkingv1.AddToScheme(scheme))
	must(v1alpha1.AddToScheme(scheme))

	ctx := ctrl.SetupSignalHandler()

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                  scheme,
		HealthProbeBindAddress:  ":8081",
		LeaderElection:          cfg.EnableLeaderElection,
		LeaderElectionID:        "titlis-operator.titlis.io",
		LeaderElectionNamespace: cfg.LeaderElectionNamespace,
	})
	must(err)

	// --- cluster name resolution ---
	directClient, err := client.New(mgr.GetConfig(), client.Options{Scheme: scheme})
	if err != nil {
		setupLog.Error(err, "failed to create client for cluster name resolution, using env/default")
	} else {
		resolvedName, source := cluster.ResolveClusterName(ctx, directClient, cfg.KubernetesNamespace)
		cfg.KubernetesClusterName = resolvedName
		setupLog.Info("cluster name resolved", "name", resolvedName, "source", source)
	}

	// --- titlis-api client ---
	var titlisClient *titlisapi.Client
	if cfg.TitlisAPIEnabled {
		setupLog.Info("waiting for titlis-api", "url", cfg.TitlisAPIBaseURL())
		if err := waitForTitlisAPI(ctx, cfg.TitlisAPIBaseURL(), 60*time.Second); err != nil {
			setupLog.Error(err, "titlis-api not healthy, aborting")
			os.Exit(1)
		}
		titlisClient = titlisapi.New(&cfg)
	}

	// --- notification ---
	var notifier notification.Notifier
	if cfg.SlackEnabled {
		notifier = slacknotifier.NewClient(&cfg)
	}

	// --- scorecard ---
	scorecardCfg := scorecard.LoadConfig(cfg.ScorecardConfigPath)

	if cfg.EnableScorecardController {
		must((&controller.ScorecardController{
			Client:     mgr.GetClient(),
			Exclusions: scorecard.NewExclusionFilter(scorecardCfg),
			TitlisAPI:  titlisClient,
			Settings:   &cfg,
		}).SetupWithManager(mgr))
		setupLog.Info("scorecard controller registered")
	}

	if cfg.EnableSLOController {
		ddSLO := ddatadog.NewSLOClient(&cfg)
		sloSvc := slo.NewService(ddSLO, ddSLO, &cfg)

		must((&controller.SLOConfigController{
			Client:    mgr.GetClient(),
			SloSvc:    sloSvc,
			Notifier:  notifier,
			TitlisAPI: titlisClient,
			Settings:  &cfg,
		}).SetupWithManager(mgr))
		setupLog.Info("sloconfig controller registered")

		if titlisClient != nil {
			pendingInterval := time.Duration(cfg.SLOPendingPollSeconds) * time.Second
			must(mgr.Add(&controller.SLOPendingRunner{
				TitlisAPI: titlisClient,
				K8s:       mgr.GetClient(),
				Interval:  pendingInterval,
				Log:       ctrl.Log.WithName("slopending"),
			}))
		}
	}

	if cfg.SyntheticEnabled {
		ddMetrics := ddatadog.NewMetricsClient(&cfg)
		siteChecks, jsonChecks := synthetic.LoadChecks(&cfg)
		must(mgr.Add(&syntheticRunnable{
			siteChecks: siteChecks,
			jsonChecks: jsonChecks,
			metrics:    ddMetrics,
		}))
		setupLog.Info("synthetic monitor registered",
			"site_checks", len(siteChecks), "json_checks", len(jsonChecks))
	}

	must(mgr.AddHealthzCheck("healthz", healthz.Ping))
	must(mgr.AddReadyzCheck("readyz", healthz.Ping))

	setupLog.Info("starting manager")
	must(mgr.Start(ctx))
}

// syntheticRunnable wraps synthetic.Run as a manager.Runnable.
type syntheticRunnable struct {
	siteChecks []synthetic.BaseCheck
	jsonChecks []synthetic.JSONValueCheck
	metrics    synthetic.MetricSender
}

func (s *syntheticRunnable) Start(ctx context.Context) error {
	synthetic.Run(ctx, s.siteChecks, s.jsonChecks, s.metrics)
	return nil
}

func waitForTitlisAPI(ctx context.Context, baseURL string, timeout time.Duration) error {
	log := ctrl.Log.WithName("setup")
	deadline := time.Now().Add(timeout)
	backoff := 2 * time.Second
	hc := &http.Client{Timeout: 5 * time.Second}
	for time.Now().Before(deadline) {
		resp, err := hc.Get(baseURL + "/health")
		if err == nil && resp.StatusCode == http.StatusOK {
			resp.Body.Close()
			return nil
		}
		if resp != nil {
			resp.Body.Close()
		}
		log.Info("titlis-api not ready, retrying", "url", baseURL, "next_backoff", backoff)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
		}
		if backoff < 30*time.Second {
			backoff *= 2
		}
	}
	return fmt.Errorf("titlis-api não saudável após %s", timeout)
}

func must(err error) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "fatal: %v\n", err)
		os.Exit(1)
	}
}
