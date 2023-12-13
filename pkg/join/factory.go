// Copyright Contributors to the Open Cluster Management project
package join

import (
	"context"
	"fmt"

	"github.com/openshift/library-go/pkg/assets"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"
	"github.com/qiujian16/capi-importer/pkg/join/scenario"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	operatorclient "open-cluster-management.io/api/client/operator/clientset/versioned"
	operatorv1 "open-cluster-management.io/api/operator/v1"
)

var (
	genericScheme = runtime.NewScheme()
	genericCodecs = serializer.NewCodecFactory(genericScheme)
	genericCodec  = genericCodecs.UniversalDeserializer()
)

func init() {
	utilruntime.Must(appsv1.AddToScheme(genericScheme))
	utilruntime.Must(operatorv1.Install(genericScheme))
}

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
	kubeClient, apiExtensionClient, operatorClient, err := b.getClients()
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

	assetFunc := func(name string) ([]byte, error) {
		template, err := scenario.Files.ReadFile(name)
		if err != nil {
			return nil, err
		}
		objData := assets.MustCreateAssetFromTemplate(name, template, b.values).Data
		return objData, nil
	}

	clientHolder := resourceapply.NewKubeClientHolder(kubeClient).WithAPIExtensionsClient(apiExtensionClient)
	applyResults := resourceapply.ApplyDirectly(
		ctx,
		clientHolder,
		recorder,
		b.cache,
		assetFunc,
		files...,
	)

	var errs []error
	for _, result := range applyResults {
		if result.Error != nil {
			errs = append(errs, err)
		}
	}

	_, err = b.applyDeployment(ctx, kubeClient, assetFunc, recorder, "join/operator.yaml")
	if err != nil {
		errs = append(errs, err)
	}

	_, err = b.applyKlusterlet(ctx, operatorClient, assetFunc, recorder, "join/klusterlets.cr.yaml")
	if err != nil {
		errs = append(errs, err)
	}

	return utilerrors.NewAggregate(errs)
}

func (b *Builder) getClients() (
	kubeClient kubernetes.Interface,
	apiExtensionsClient apiextensionsclient.Interface,
	operatorClient operatorclient.Interface,
	err error) {
	config, err := b.spokeKubeConfig.ClientConfig()
	if err != nil {
		return nil, nil, nil, err
	}
	kubeClient, err = kubernetes.NewForConfig(config)
	if err != nil {
		return
	}
	operatorClient, err = operatorclient.NewForConfig(config)
	if err != nil {
		return
	}

	apiExtensionsClient, err = apiextensionsclient.NewForConfig(config)
	if err != nil {
		return
	}
	return
}

func (b *Builder) applyDeployment(
	ctx context.Context,
	client kubernetes.Interface,
	manifests resourceapply.AssetFunc,
	recorder events.Recorder, file string) (*appsv1.Deployment, error) {
	deploymentBytes, err := manifests(file)
	if err != nil {
		return nil, err
	}
	deployment, _, err := genericCodec.Decode(deploymentBytes, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("%q: %v", file, err)
	}

	updatedDeployment, _, err := resourceapply.ApplyDeployment(
		ctx,
		client.AppsV1(),
		recorder,
		deployment.(*appsv1.Deployment), 0)
	if err != nil {
		return updatedDeployment, fmt.Errorf("%q (%T): %v", file, deployment, err)
	}

	return updatedDeployment, nil
}

func (b *Builder) applyKlusterlet(
	ctx context.Context,
	client operatorclient.Interface,
	manifests resourceapply.AssetFunc,
	recorder events.Recorder, file string) (*operatorv1.Klusterlet, error) {
	klusterletBytes, err := manifests(file)
	if err != nil {
		return nil, err
	}
	object, _, err := genericCodec.Decode(klusterletBytes, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("%q: %v", file, err)
	}

	desired := object.(*operatorv1.Klusterlet)

	existing, err := client.OperatorV1().Klusterlets().Get(ctx, desired.Name, metav1.GetOptions{})
	if errors.IsNotFound(err) {
		created, createErr := client.OperatorV1().Klusterlets().Create(ctx, desired, metav1.CreateOptions{})
		return created, createErr
	} else if err != nil {
		return nil, err
	}

	if equality.Semantic.DeepEqual(existing.Spec, desired.Spec) {
		return existing, nil
	}

	existing.Spec = desired.Spec

	updated, err := client.OperatorV1().Klusterlets().Update(ctx, existing, metav1.UpdateOptions{})
	return updated, err
}
