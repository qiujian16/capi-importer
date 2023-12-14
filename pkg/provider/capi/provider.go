package capi

import (
	"context"
	"fmt"
	"time"

	"github.com/pkg/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
)

type CAPIProvider struct {
	informer   dynamicinformer.DynamicSharedInformerFactory
	lister     cache.GenericLister
	kubeClient kubernetes.Interface
}

var gvr = schema.GroupVersionResource{}

func NewCAPIProvider(kubeconfig *rest.Config) *CAPIProvider {
	dynamicClient := dynamic.NewForConfigOrDie(kubeconfig)
	kubeClient := kubernetes.NewForConfigOrDie(kubeconfig)

	dynamicInformer := dynamicinformer.NewDynamicSharedInformerFactory(dynamicClient, 30*time.Minute)
	return &CAPIProvider{
		informer:   dynamicInformer,
		lister:     dynamicInformer.ForResource(gvr).Lister(),
		kubeClient: kubeClient,
	}
}

func (c *CAPIProvider) AddEventHandler(handler cache.ResourceEventHandler) (cache.ResourceEventHandlerRegistration, error) {
	return c.informer.ForResource(gvr).Informer().AddEventHandler(handler)
}

func (c *CAPIProvider) HasSynced() bool {
	return c.informer.ForResource(gvr).Informer().HasSynced()
}

func (c *CAPIProvider) Key(obj runtime.Object) []string {
	name, _ := cache.MetaNamespaceKeyFunc(obj)
	return []string{fmt.Sprintf("%s/%s", c.Name(), name)}
}

func (c *CAPIProvider) Name() string {
	return "capiaws"
}

func (c *CAPIProvider) Start(ctx context.Context) {
	c.informer.Start(ctx.Done())
}

func (c *CAPIProvider) KubeConfig(clusterKey string) (clientcmd.ClientConfig, error) {
	namespace, name, err := cache.SplitMetaNamespaceKey(clusterKey)
	if err != nil {
		return nil, err
	}
	_, err = c.lister.ByNamespace(namespace).Get(name)
	if err != nil {
		return nil, err
	}

	secret, err := c.kubeClient.CoreV1().Secrets(namespace).Get(context.TODO(), name+"-kubeconfig", metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	data, ok := secret.Data["value"]
	if !ok {
		return nil, errors.Errorf("missing key %q in secret data", name)
	}
	return clientcmd.NewClientConfigFromBytes(data)
}
