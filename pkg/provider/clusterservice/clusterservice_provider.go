package clusterservice

import (
	"context"
	"fmt"
	"time"

	sdk "github.com/openshift-online/ocm-sdk-go"
	clustersmgmtv1 "github.com/openshift-online/ocm-sdk-go/clustersmgmt/v1"
	"github.com/qiujian16/capi-importer/pkg/provider"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	clusterapiv1 "open-cluster-management.io/api/cluster/v1"
)

type ClusterServiceProvider struct {
	handler cache.ResourceEventHandler
	store   cache.Store
	token   string
}

func NewClusterServiceProvider(client kubernetes.Interface) provider.ClusterProvider {
	return &ClusterServiceProvider{
		store: cache.NewStore(clusterKey),
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
		return
	}

	// Create the connection, and remember to close it:
	connection, err := sdk.NewConnectionBuilder().
		Logger(logger).
		Tokens(c.token).
		Build()
	if err != nil {
		return
	}
	defer connection.Close()

	// Get the client for the resource that manages the collection of clusters:
	collection := connection.ClustersMgmt().V1().Clusters()
	clusterList, err := collection.List().Send()
	if err != nil {
		return
	}

	clusterList.Items().Each(func(cluster *clustersmgmtv1.Cluster) bool {
		// convert cluster to ManagedCluster
		mcl := &clusterapiv1.ManagedCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name: cluster.Name(),
			},
		}
		if _, exists, _ := c.store.Get(mcl); exists {
			return true
		}
		c.store.Add(mcl)
		c.handler.OnAdd(mcl, false)
		return true
	})
}

func clusterKey(obj interface{}) (string, error) {
	accesor, err := meta.Accessor(obj)
	return accesor.GetName(), err
}
