//go:build integration

package integration_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	networkingv1 "k8s.io/api/networking/v1"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"k8s.io/apimachinery/pkg/runtime"

	"github.com/titlis/operator/api/v1alpha1"
	"github.com/titlis/operator/internal/config"
	"github.com/titlis/operator/internal/controller"
	k8swriter "github.com/titlis/operator/internal/k8s"
	"github.com/titlis/operator/internal/notification"
	"github.com/titlis/operator/internal/scorecard"
)

var (
	k8sClient  client.Client
	testEnv    *envtest.Environment
	ctx        context.Context
	cancel     context.CancelFunc
	testScheme *runtime.Scheme
)

// defaultTestSettings provides a minimal config for integration tests.
func defaultTestSettings() *config.Settings {
	return &config.Settings{
		KubernetesNamespace:      "default",
		KubernetesClusterName:    "test-cluster",
		EnableScorecardController: true,
		EnableSLOController:       false,
		ReconcileIntervalSeconds:  30,
		EnableLeaderElection:      false,
		SlackEnabled:              false,
		TitlisAPIEnabled:          false,
		ScorecardConfigPath:       "../../../config/scorecard-config.yaml",
	}
}

func TestIntegration(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Integration Suite")
}

var _ = BeforeSuite(func() {
	ctrl.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))
	ctx, cancel = context.WithCancel(context.Background())

	By("bootstrapping envtest")
	testEnv = &envtest.Environment{
		CRDDirectoryPaths: []string{
			filepath.Join("..", "..", "..", "charts", "titlis-operator", "crds"),
		},
		ErrorIfCRDPathMissing: true,
		BinaryAssetsDirectory: os.Getenv("KUBEBUILDER_ASSETS"),
	}

	cfg, err := testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())

	testScheme = runtime.NewScheme()
	Expect(clientgoscheme.AddToScheme(testScheme)).To(Succeed())
	Expect(appsv1.AddToScheme(testScheme)).To(Succeed())
	Expect(autoscalingv2.AddToScheme(testScheme)).To(Succeed())
	Expect(networkingv1.AddToScheme(testScheme)).To(Succeed())
	Expect(v1alpha1.AddToScheme(testScheme)).To(Succeed())

	k8sClient, err = client.New(cfg, client.Options{Scheme: testScheme})
	Expect(err).NotTo(HaveOccurred())

	mgr, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme:               testScheme,
		LeaderElection:       false,
		HealthProbeBindAddress: "0",
		MetricsBindAddress:   "0",
	})
	Expect(err).NotTo(HaveOccurred())

	settings := defaultTestSettings()
	scorecardCfg := scorecard.LoadConfig(settings.ScorecardConfigPath)
	scorecardSvc := scorecard.NewService(scorecardCfg, mgr.GetClient())
	aswWriter := k8swriter.NewAppScorecardWriter(mgr.GetClient(), mgr.GetScheme())
	buffer := notification.NewNamespaceBuffer(15, 10)

	Expect((&controller.ScorecardController{
		Client:       mgr.GetClient(),
		ScorecardSvc: scorecardSvc,
		AswWriter:    aswWriter,
		Notifier:     nil,
		Buffer:       buffer,
		Settings:     settings,
	}).SetupWithManager(mgr)).To(Succeed())

	go func() {
		defer GinkgoRecover()
		Expect(mgr.Start(ctx)).To(Succeed())
	}()
})

var _ = AfterSuite(func() {
	cancel()
	By("tearing down envtest")
	Eventually(func() error {
		return testEnv.Stop()
	}, 10*time.Second, 250*time.Millisecond).Should(Succeed())
})
