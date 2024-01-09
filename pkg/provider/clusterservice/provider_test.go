package clusterservice

import (
	"context"
	"testing"
	"time"

	clustersmgmtv1 "github.com/openshift-online/ocm-sdk-go/clustersmgmt/v1"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	clientcmdapi "k8s.io/client-go/tools/clientcmd"
	clusterapiv1 "open-cluster-management.io/api/cluster/v1"
)

var token = ""

func TestPoll(t *testing.T) {

	csProvider := &ClusterServiceProvider{
		store: cache.NewIndexer(clusterKey, cache.Indexers{
			byKey: cache.MetaNamespaceIndexFunc,
		}),
		token:         token,
		clusterAdmins: make(map[string]*clustersmgmtv1.HTPasswdIdentityProvider),
		handler: &cache.ResourceEventHandlerFuncs{
			AddFunc:    func(obj interface{}) {},
			UpdateFunc: func(oldObj, newObj interface{}) {},
			DeleteFunc: func(obj interface{}) {},
		},
	}

	assert.Eventually(t, func() bool {
		csProvider.poll()

		for _, item := range csProvider.store.List() {
			mc, ok := item.(*clusterapiv1.ManagedCluster)
			if !ok {
				return false
			}

			kubeConfig, ok := mc.Annotations["kubeconfig"]
			if !ok {
				return false
			}

			config, err := clientcmdapi.Load([]byte(kubeConfig))
			if err != nil {
				t.Fatal(err)
			}

			clientConfig := &rest.Config{
				Host:            config.Clusters["default-cluster"].Server,
				BearerToken:     config.AuthInfos["default-auth"].Token,
				TLSClientConfig: rest.TLSClientConfig{Insecure: true},
			}

			kubeClient, err := kubernetes.NewForConfig(clientConfig)
			if err != nil {
				t.Fatal(err)
			}

			_, err = kubeClient.CoreV1().Namespaces().List(context.TODO(), metav1.ListOptions{})
			if err != nil {
				t.Fatal(err)
			}

		}

		return false
	}, 10*time.Minute, 30*time.Second)
}
