// Copyright Contributors to the Open Cluster Management project
package join

import (
	"context"
	"fmt"

	"github.com/ghodss/yaml"
	authv1 "k8s.io/api/authentication/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	clientcmdapiv1 "k8s.io/client-go/tools/clientcmd/api/v1"
	"k8s.io/utils/pointer"
)

type BootstrapGetter interface {
	KubeConfig() (clientcmdapiv1.Config, error)
	KubeConfigRaw() ([]byte, error)
}

type BootstrapConfig struct {
	CA           []byte
	HubAPIServer string
	SAName       string
	SANamespace  string
}

type TokenBootStrapper struct {
	config BootstrapConfig
	client kubernetes.Interface
}

func NewTokenBootStrapper(config BootstrapConfig, client kubernetes.Interface) BootstrapGetter {
	return &TokenBootStrapper{
		config: config,
		client: client,
	}
}

func (g *TokenBootStrapper) KubeConfigRaw() ([]byte, error) {
	clientConfig, err := g.KubeConfig()
	if err != nil {
		return nil, err
	}

	bootstrapConfigBytes, err := yaml.Marshal(clientConfig)
	if err != nil {
		return nil, err
	}

	return bootstrapConfigBytes, nil
}

func (g *TokenBootStrapper) KubeConfig() (clientcmdapiv1.Config, error) {
	saToken, err := g.client.CoreV1().ServiceAccounts(g.config.SANamespace).CreateToken(
		context.TODO(),
		g.config.SAName,
		&authv1.TokenRequest{
			Spec: authv1.TokenRequestSpec{
				ExpirationSeconds: pointer.Int64(8640 * 3600),
			},
		},
		metav1.CreateOptions{})
	if err != nil {
		return clientcmdapiv1.Config{}, err
	}

	clientConfig := clientcmdapiv1.Config{
		// Define a cluster stanza based on the bootstrap kubeconfig.
		Clusters: []clientcmdapiv1.NamedCluster{
			{
				Name: "hub",
				Cluster: clientcmdapiv1.Cluster{
					Server: g.config.HubAPIServer,
				},
			},
		},
		// Define auth based on the obtained client cert.
		AuthInfos: []clientcmdapiv1.NamedAuthInfo{
			{
				Name: "bootstrap",
				AuthInfo: clientcmdapiv1.AuthInfo{
					Token: saToken.Status.Token,
				},
			},
		},
		// Define a context that connects the auth info and cluster, and set it as the default
		Contexts: []clientcmdapiv1.NamedContext{
			{
				Name: "bootstrap",
				Context: clientcmdapiv1.Context{
					Cluster:   "hub",
					AuthInfo:  "bootstrap",
					Namespace: "default",
				},
			},
		},
		CurrentContext: "bootstrap",
	}

	if g.config.CA != nil {
		// directly set ca-data if --ca-file is set
		clientConfig.Clusters[0].Cluster.CertificateAuthorityData = g.config.CA
	} else {
		// get ca data from, ca may empty(cluster-info exists with no ca data)
		ca, err := getCACert(g.client)
		if err != nil {
			return clientConfig, err
		}
		clientConfig.Clusters[0].Cluster.CertificateAuthorityData = ca
	}

	return clientConfig, nil
}

func getCACert(kubeClient kubernetes.Interface) ([]byte, error) {
	config, err := getClusterInfoKubeConfig(kubeClient)
	if err == nil {
		clusters := config.Clusters
		if len(clusters) != 1 {
			return nil, fmt.Errorf("can not find the cluster in the cluster-info")
		}
		cluster := clusters[0].Cluster
		return cluster.CertificateAuthorityData, nil
	}
	if errors.IsNotFound(err) {
		cm, err := kubeClient.CoreV1().ConfigMaps("kube-public").Get(context.TODO(), "kube-root-ca.crt", metav1.GetOptions{})
		if err == nil {
			return []byte(cm.Data["ca.crt"]), nil
		}
		if errors.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	return nil, err
}

func getClusterInfoKubeConfig(kubeClient kubernetes.Interface) (*clientcmdapiv1.Config, error) {
	cm, err := kubeClient.CoreV1().ConfigMaps("kube-public").Get(context.TODO(), "cluster-info", metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	config := &clientcmdapiv1.Config{}
	err = yaml.Unmarshal([]byte(cm.Data["kubeconfig"]), config)
	if err != nil {
		return nil, err
	}
	return config, nil
}
