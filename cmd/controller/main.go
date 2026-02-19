package main

import (
	"log/slog"
	"os"

	iafv1alpha1 "github.com/dlapiduz/iaf/api/v1alpha1"
	"github.com/dlapiduz/iaf/internal/config"
	"github.com/dlapiduz/iaf/internal/controller"
	"github.com/dlapiduz/iaf/internal/k8s"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)
	ctrl.SetLogger(zap.New())

	cfg, err := config.Load()
	if err != nil {
		logger.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(iafv1alpha1.AddToScheme(scheme))

	restConfig, err := k8s.GetConfig(cfg.KubeConfig)
	if err != nil {
		logger.Error("failed to get kubernetes config", "error", err)
		os.Exit(1)
	}

	mgr, err := ctrl.NewManager(restConfig, ctrl.Options{
		Scheme: scheme,
	})
	if err != nil {
		logger.Error("failed to create manager", "error", err)
		os.Exit(1)
	}

	reconciler := &controller.ApplicationReconciler{
		Client:         mgr.GetClient(),
		Scheme:         mgr.GetScheme(),
		ClusterBuilder: cfg.ClusterBuilder,
		RegistryPrefix: cfg.RegistryPrefix,
		BaseDomain:     cfg.BaseDomain,
	}

	if err := reconciler.SetupWithManager(mgr); err != nil {
		logger.Error("failed to setup controller", "error", err)
		os.Exit(1)
	}

	logger.Info("starting controller manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		logger.Error("controller manager exited with error", "error", err)
		os.Exit(1)
	}
}
