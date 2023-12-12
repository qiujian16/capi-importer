package importers

import (
	"context"
	"os"
	"time"

	"github.com/openshift/library-go/pkg/controller/controllercmd"
	"github.com/qiujian16/capi-importer/pkg/importers/controllers"
	"github.com/qiujian16/capi-importer/pkg/join"
	"github.com/qiujian16/capi-importer/pkg/provider"
	"github.com/qiujian16/capi-importer/pkg/provider/clusterservice"
	"github.com/spf13/pflag"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	clusterv1client "open-cluster-management.io/api/client/cluster/clientset/versioned"
	clusterinformers "open-cluster-management.io/api/client/cluster/informers/externalversions"
)

type ImporterOptions struct {
	HubAPIServer string
	CAFile       string
	CSToken      string
	SA           string
}

func NewImporterOptions() *ImporterOptions {
	return &ImporterOptions{}
}

// AddFlags registers flags for manager
func (o *ImporterOptions) AddFlags(fs *pflag.FlagSet) {
	fs.StringVar(&o.HubAPIServer, "hub-apiserver", o.HubAPIServer, "")
	fs.StringVar(&o.CAFile, "hub-ca-file", o.CAFile, "")
	fs.StringVar(&o.SA, "bootstrap-sa", o.SA, "")
	fs.StringVar(&o.CSToken, "cluster-service-token", o.CSToken, "")
}

func (o *ImporterOptions) RunImporterController(ctx context.Context, controllerContext *controllercmd.ControllerContext) error {
	// Build kubclient client and informer for managed cluster
	kubeClient, err := kubernetes.NewForConfig(controllerContext.KubeConfig)
	if err != nil {
		return err
	}
	clusterClient, err := clusterv1client.NewForConfig(controllerContext.KubeConfig)
	if err != nil {
		return err
	}
	clusterInformers := clusterinformers.NewSharedInformerFactory(clusterClient, 30*time.Minute)

	saNamespace, saName, err := cache.SplitMetaNamespaceKey(o.SA)
	if err != nil {
		return err
	}

	caData, err := os.ReadFile(o.CAFile)
	if err != nil {
		return err
	}
	bootStrapConfig := join.BootstrapConfig{
		HubAPIServer: o.HubAPIServer,
		SANamespace:  saNamespace,
		SAName:       saName,
		CA:           caData,
	}

	providers := []provider.ClusterProvider{
		clusterservice.NewClusterServiceProvider(o.CSToken),
	}

	ctrl := controllers.NewController(
		kubeClient,
		clusterClient,
		clusterInformers.Cluster().V1().ManagedClusters(),
		bootStrapConfig,
		controllerContext.EventRecorder,
		providers...,
	)

	go clusterInformers.Start(ctx.Done())
	for _, p := range providers {
		go p.Start(ctx)
	}
	go ctrl.Run(ctx, 1)
	return nil
}
