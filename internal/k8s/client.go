package k8s

import (
	"fmt"

	iafv1alpha1 "github.com/dlapiduz/iaf/api/v1alpha1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// NewScheme creates a runtime scheme with all required types registered.
func NewScheme() *runtime.Scheme {
	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(iafv1alpha1.AddToScheme(scheme))
	return scheme
}

// NewClient creates a controller-runtime client for the given kubeconfig path.
// If kubeconfig is empty, it uses in-cluster config.
func NewClient(kubeconfig string) (client.Client, error) {
	cfg, err := GetConfig(kubeconfig)
	if err != nil {
		return nil, fmt.Errorf("getting kubernetes config: %w", err)
	}

	scheme := NewScheme()
	c, err := client.New(cfg, client.Options{Scheme: scheme})
	if err != nil {
		return nil, fmt.Errorf("creating kubernetes client: %w", err)
	}
	return c, nil
}

// GetConfig returns a Kubernetes REST config.
func GetConfig(kubeconfig string) (*rest.Config, error) {
	if kubeconfig != "" {
		return clientcmd.BuildConfigFromFlags("", kubeconfig)
	}
	// Try in-cluster first, then fallback to default kubeconfig
	cfg, err := rest.InClusterConfig()
	if err != nil {
		loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
		configOverrides := &clientcmd.ConfigOverrides{}
		kubeConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, configOverrides)
		return kubeConfig.ClientConfig()
	}
	return cfg, nil
}
