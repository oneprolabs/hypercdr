package kube

import (
	"strings"

	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

func BuildRESTConfig(kubeconfigPath string) (*rest.Config, error) {
	var cfg *rest.Config
	var err error
	if strings.TrimSpace(kubeconfigPath) != "" {
		cfg, err = clientcmd.BuildConfigFromFlags("", kubeconfigPath)
	} else {
		cfg, err = rest.InClusterConfig()
		if err != nil {
			loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
			cfg, err = clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, &clientcmd.ConfigOverrides{}).ClientConfig()
		}
	}
	if err != nil {
		return nil, err
	}
	return tuneRESTConfig(cfg), nil
}

// Resource discovery lists many API types in parallel. The client-go defaults
// (5 QPS, burst 10) turn that bounded work into 20+ seconds of local
// throttling on clusters with CRDs and aggregated APIs. These limits keep the
// eight-worker scan bounded while avoiding artificial client-side delays.
func tuneRESTConfig(cfg *rest.Config) *rest.Config {
	cfg.QPS = 50
	cfg.Burst = 100
	return cfg
}
