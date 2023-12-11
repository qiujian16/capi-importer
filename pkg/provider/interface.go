package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/openshift/library-go/pkg/controller/factory"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/clientcmd"
)

type ClusterProvider interface {
	factory.Informer

	// key is formated as providerName/namespace/name
	Key(obj runtime.Object) []string

	KubeConfig(clusterKey string) (clientcmd.ClientConfig, error)

	Name() string

	Start(ctx context.Context)
}

func ParseKey(key string) (string, string, error) {
	s := strings.SplitN(key, "/", 1)
	if len(s) < 2 {
		return "", "", fmt.Errorf("key %s format is not correct", key)
	}
	return s[0], s[1], nil
}
