// Copyright Contributors to the Open Cluster Management project
package join

import (
	"context"

	"github.com/openshift/library-go/pkg/assets"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"
	"github.com/qiujian16/capi-importer/pkg/join/scenario"
	corev1 "k8s.io/api/core/v1"
	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/discovery/cached/memory"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"
	"k8s.io/client-go/tools/clientcmd"
	operatorv1 "open-cluster-management.io/api/operator/v1"
)

type Builder struct {
	values          Values
	cache           resourceapply.ResourceCache
	spokeKubeConfig clientcmd.ClientConfig
}

// Values: The values used in the template
type Values struct {
	//ClusterName: the name of the joined cluster on the hub
	ClusterName string
	//AgentNamespace: the namespace to deploy the agent
	AgentNamespace string
	//Hub: Hub information
	Hub Hub
	//Klusterlet is the klusterlet related configuration
	Klusterlet Klusterlet
	//Registry is the image registry related configuration
	Registry string
	//bundle version
	BundleVersion BundleVersion
	// managed kubeconfig
	ManagedKubeconfig string

	// Features is the slice of feature for registration
	RegistrationFeatures []operatorv1.FeatureGate

	// Features is the slice of feature for work
	WorkFeatures []operatorv1.FeatureGate
}

// Hub: The hub values for the template
type Hub struct {
	//APIServer: The API Server external URL
	APIServer string
	//KubeConfig: The kubeconfig of the bootstrap secret to connect to the hub
	KubeConfig string
}

// Klusterlet is for templating klusterlet configuration
type Klusterlet struct {
	//APIServer: The API Server external URL
	APIServer string
	Mode      string
	Name      string
}

type BundleVersion struct {
	// registration image version
	RegistrationImageVersion string
	// placement image version
	PlacementImageVersion string
	// work image version
	WorkImageVersion string
	// operator image version
	OperatorImageVersion string
}

func NewBuilder() *Builder {
	return &Builder{
		cache: resourceapply.NewResourceCache(),
	}
}

func (b *Builder) WithValues(v Values) *Builder {
	b.values = v
	return b
}

func (b *Builder) WithSpokeKubeConfig(config clientcmd.ClientConfig) *Builder {
	b.spokeKubeConfig = config
	return b
}

func (b *Builder) ApplyImport(ctx context.Context, recorder events.Recorder) error {
	kubeClient, apiExtensionClient, _, err := b.getClients()
	if err != nil {
		return err
	}

	_, err = kubeClient.CoreV1().Namespaces().Get(ctx, b.values.AgentNamespace, metav1.GetOptions{})
	if errors.IsNotFound(err) {
		_, err = kubeClient.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: b.values.AgentNamespace,
				Annotations: map[string]string{
					"workload.openshift.io/allowed": "management",
				},
			},
		}, metav1.CreateOptions{})
		if err != nil {
			return err
		}
	} else if err != nil {
		return err
	}

	var files []string
	files = append(files,
		"join/klusterlets.crd.yaml",
		"join/namespace.yaml",
		"join/service_account.yaml",
		"join/cluster_role.yaml",
		"join/cluster_role_binding.yaml",
		"bootstrap_hub_kubeconfig.yaml",
	)

	clientHolder := resourceapply.NewKubeClientHolder(kubeClient).WithAPIExtensionsClient(apiExtensionClient)
	applyResults := resourceapply.ApplyDirectly(
		ctx,
		clientHolder,
		recorder,
		b.cache,
		func(name string) ([]byte, error) {
			template, err := scenario.Files.ReadFile(name)
			if err != nil {
				return nil, err
			}
			objData := assets.MustCreateAssetFromTemplate(name, template, b.values).Data
			return objData, nil
		},
		files...,
	)

	var errs []error
	for _, result := range applyResults {
		if result.Error != nil {
			errs = append(errs, err)
		}
	}

	// TODO create operator

	return utilerrors.NewAggregate(errs)
}

func (b *Builder) ToRESTConfig() (*rest.Config, error) {
	return b.spokeKubeConfig.ClientConfig()
}

func (b *Builder) ToDiscoveryClient() (discovery.CachedDiscoveryInterface, error) {
	restconfig, err := b.spokeKubeConfig.ClientConfig()
	if err != nil {
		return nil, err
	}
	dc, err := discovery.NewDiscoveryClientForConfig(restconfig)
	if err != nil {
		return nil, err
	}
	return memory.NewMemCacheClient(dc), nil
}

func (b *Builder) ToRESTMapper() (meta.RESTMapper, error) {
	dc, err := b.ToDiscoveryClient()
	if err != nil {
		return nil, err
	}
	return restmapper.NewDeferredDiscoveryRESTMapper(dc), nil
}

func (b *Builder) ToRawKubeConfigLoader() clientcmd.ClientConfig {
	return b.spokeKubeConfig
}

func (b *Builder) getClients() (
	kubeClient kubernetes.Interface,
	apiExtensionsClient apiextensionsclient.Interface,
	dynamicClient dynamic.Interface,
	err error) {
	config, err := b.spokeKubeConfig.ClientConfig()
	if err != nil {
		return nil, nil, nil, err
	}
	kubeClient, err = kubernetes.NewForConfig(config)
	if err != nil {
		return
	}
	dynamicClient, err = dynamic.NewForConfig(config)
	if err != nil {
		return
	}

	apiExtensionsClient, err = apiextensionsclient.NewForConfig(config)
	if err != nil {
		return
	}
	return
}
