package clusterservice

import (
	"context"
	"fmt"
	"time"

	sdk "github.com/openshift-online/ocm-sdk-go"
	clustersmgmtv1 "github.com/openshift-online/ocm-sdk-go/clustersmgmt/v1"
	"github.com/openshift-online/rh-trex/pkg/errors"
	"github.com/qiujian16/capi-importer/pkg/provider"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	clusterapiv1 "open-cluster-management.io/api/cluster/v1"
)

const byKey = "by-key"

type ClusterServiceProvider struct {
	handler cache.ResourceEventHandler
	store   cache.Store
	token   string
}

func NewClusterServiceProvider(token string) provider.ClusterProvider {
	return &ClusterServiceProvider{
		store: cache.NewIndexer(clusterKey, cache.Indexers{
			byKey: cache.MetaNamespaceIndexFunc,
		}),
		token: token,
	}
}

func (c *ClusterServiceProvider) AddEventHandler(handler cache.ResourceEventHandler) (cache.ResourceEventHandlerRegistration, error) {
	c.handler = handler
	return c, nil
}

func (c *ClusterServiceProvider) HasSynced() bool {
	return true
}

func (c *ClusterServiceProvider) Key(obj runtime.Object) []string {
	name, _ := clusterKey(obj)
	return []string{fmt.Sprintf("%s/%s", c.Name(), name)}
}

func (c *ClusterServiceProvider) KubeConfig(clusterKey string) (clientcmd.ClientConfig, error) {
	cluster, exist, err := c.store.GetByKey(clusterKey)
	if err != nil {
		return nil, err
	}
	if !exist {
		return nil, errors.NotFound("cluster is not found")
	}
	accesor, _ := meta.Accessor(cluster)
	configString := accesor.GetAnnotations()["kubeconfig"]
	return clientcmd.NewClientConfigFromBytes([]byte(configString))
}

func (c *ClusterServiceProvider) Name() string {
	return "clusterservice"
}

func (c *ClusterServiceProvider) Start(ctx context.Context) {
	wait.Until(c.poll, 10*time.Minute, ctx.Done())
}

func (c *ClusterServiceProvider) poll() {
	logger, err := sdk.NewGoLoggerBuilder().
		Debug(true).
		Build()
	if err != nil {
		logger.Error(context.TODO(), err.Error())
		return
	}

	// Create the connection, and remember to close it:
	connection, err := sdk.NewConnectionBuilder().
		Logger(logger).
		Tokens(c.token).
		Build()
	if err != nil {
		logger.Error(context.TODO(), err.Error())
		return
	}
	defer connection.Close()

	// Get the client for the resource that manages the collection of clusters:
	collection := connection.ClustersMgmt().V1().Clusters()
	clusterList, err := collection.List().Send()
	if err != nil {
		logger.Error(context.TODO(), err.Error())
		return
	}

	clusterList.Items().Each(func(cluster *clustersmgmtv1.Cluster) bool {
		// convert cluster to ManagedCluster
		mcl := &clusterapiv1.ManagedCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name: cluster.Name(),
				Annotations: map[string]string{
					"id": cluster.ID(),
				},
			},
		}
		clusterCredential, err := collection.Cluster(cluster.ID()).Credentials().Get().Send()
		if err != nil {
			logger.Error(context.TODO(), err.Error())
			return true
		}
		mcl.Annotations["kubeconfig"] = clusterCredential.Body().Kubeconfig()
		if _, exists, _ := c.store.GetByKey(mcl.Name); exists {
			return true
		}
		if err := c.store.Add(mcl); err != nil {
			logger.Error(context.TODO(), err.Error())
			return true
		}
		c.handler.OnAdd(mcl, false)
		return true
	})
}

func clusterKey(obj interface{}) (string, error) {
	accesor, err := meta.Accessor(obj)
	return accesor.GetName(), err
}
