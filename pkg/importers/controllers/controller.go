package controllers

import (
	"context"
	"encoding/base64"
	"fmt"

	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"
	"github.com/qiujian16/capi-importer/pkg/join"
	"github.com/qiujian16/capi-importer/pkg/provider"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	clusterclient "open-cluster-management.io/api/client/cluster/clientset/versioned"
	clusterinformerv1 "open-cluster-management.io/api/client/cluster/informers/externalversions/cluster/v1"
	clusterlisterv1 "open-cluster-management.io/api/client/cluster/listers/cluster/v1"
	operatorv1 "open-cluster-management.io/api/operator/v1"
)

type controller struct {
	kubeClient      kubernetes.Interface
	clusterClient   clusterclient.Interface
	clusterLister   clusterlisterv1.ManagedClusterLister
	bootstrapConfig join.BootstrapConfig
	cache           resourceapply.ResourceCache
	providers       map[string]provider.ClusterProvider
}

func NewController(
	kubeClient kubernetes.Interface,
	clusterClient clusterclient.Interface,
	clusterInformer clusterinformerv1.ManagedClusterInformer,
	bootstrapConfig join.BootstrapConfig,
	recorder events.Recorder,
	providers ...provider.ClusterProvider) factory.Controller {

	c := &controller{
		kubeClient:      kubeClient,
		clusterLister:   clusterInformer.Lister(),
		clusterClient:   clusterClient,
		cache:           resourceapply.NewResourceCache(),
		providers:       map[string]provider.ClusterProvider{},
		bootstrapConfig: bootstrapConfig,
	}

	ctrl := factory.New().WithInformersQueueKeysFunc(func(obj runtime.Object) []string {
		// todo add provider name and capi ref in the key
		key, _ := cache.MetaNamespaceKeyFunc(obj)
		return []string{key}
	}, clusterInformer.Informer())

	for _, p := range providers {
		ctrl = ctrl.WithInformersQueueKeysFunc(p.Key, p)
		c.providers[p.Name()] = p
	}

	return ctrl.WithSync(c.sync).ToController("importer", recorder)
}

func (n *controller) sync(ctx context.Context, controllerContext factory.SyncContext) error {
	logger := klog.FromContext(ctx)
	key := controllerContext.QueueKey()
	logger.V(4).Info("Reconciling cluster provider", "queueKey", key)

	providerName, clusterKey, err := provider.ParseKey(key)
	if err != nil {
		return err
	}

	p, ok := n.providers[providerName]
	if !ok {
		return fmt.Errorf("provider %s does not exist", providerName)
	}

	_, clusterName, err := cache.SplitMetaNamespaceKey(clusterKey)
	if err != nil {
		return err
	}

	cluster, err := n.clusterLister.Get(clusterName)
	if err != nil && !errors.IsNotFound(err) {
		return err
	}

	// cluster is imported already, do nothing
	if err == nil && meta.IsStatusConditionTrue(cluster.Status.Conditions, "Imported") {
		return nil
	}
	cluster = cluster.DeepCopy()

	bootstrapper := join.NewTokenBootStrapper(n.bootstrapConfig, n.kubeClient)
	bootstrapKubeConfig, err := bootstrapper.KubeConfigRaw()
	if err != nil {
		return err
	}

	kubeConfig, err := p.KubeConfig(clusterKey)
	if err != nil {
		return err
	}

	values := join.Values{
		ClusterName:    clusterName,
		AgentNamespace: "open-cluster-management-agent",
		Hub: join.Hub{
			KubeConfig: base64.StdEncoding.EncodeToString(bootstrapKubeConfig),
		},
		Klusterlet: join.Klusterlet{
			Name: "klusterlet",
		},
		Registry: "quay.io/open-cluster-management-io",
		BundleVersion: join.BundleVersion{
			RegistrationImageVersion: "latest",
			WorkImageVersion:         "latest",
			OperatorImageVersion:     "latest",
		},
		RegistrationFeatures: []operatorv1.FeatureGate{},
		WorkFeatures:         []operatorv1.FeatureGate{},
	}

	builder := join.NewBuilder().WithSpokeKubeConfig(kubeConfig).WithValues(values)
	err = builder.ApplyImport(ctx, controllerContext.Recorder())

	if err != nil {
		meta.SetStatusCondition(&cluster.Status.Conditions, metav1.Condition{
			Type:    "Imported",
			Status:  metav1.ConditionFalse,
			Reason:  "ImportError",
			Message: fmt.Sprintf("Failed to import with err %v", err),
		})
	} else {
		meta.SetStatusCondition(&cluster.Status.Conditions, metav1.Condition{
			Type:    "Imported",
			Status:  metav1.ConditionTrue,
			Reason:  "ImportSucceed",
			Message: "Import succeeds",
		})
	}
	_, updateErr := n.clusterClient.ClusterV1().ManagedClusters().UpdateStatus(ctx, cluster, metav1.UpdateOptions{})
	if updateErr != nil {
		return updateErr
	}
	return err
}
