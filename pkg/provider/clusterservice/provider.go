package clusterservice

import (
	"context"
	"fmt"
	"net/http"
	"time"

	sdk "github.com/openshift-online/ocm-sdk-go"
	clustersmgmtv1 "github.com/openshift-online/ocm-sdk-go/clustersmgmt/v1"
	"github.com/openshift-online/rh-trex/pkg/errors"
	"github.com/openshift/library-go/pkg/oauth/tokenrequest"
	"github.com/qiujian16/capi-importer/pkg/provider"
	"github.com/sethvargo/go-password/password"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	clientcmdlatest "k8s.io/client-go/tools/clientcmd/api/latest"
	clusterapiv1 "open-cluster-management.io/api/cluster/v1"
)

const byKey = "by-key"

type ClusterServiceProvider struct {
	handler cache.ResourceEventHandler
	store   cache.Store
	token   string

	clusterAdmins map[string]*clustersmgmtv1.HTPasswdIdentityProvider
}

func NewClusterServiceProvider(token string) provider.ClusterProvider {
	return &ClusterServiceProvider{
		store: cache.NewIndexer(clusterKey, cache.Indexers{
			byKey: cache.MetaNamespaceIndexFunc,
		}),
		token:         token,
		clusterAdmins: make(map[string]*clustersmgmtv1.HTPasswdIdentityProvider),
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

	// TODO need more search params like cluster status, archived cluster, etc
	clusterList, err := collection.List().Search("product.id='rosa'").Send()
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

		kubeconfig, err := c.getClusterKubeconfig(collection.Cluster(cluster.ID()), cluster)
		if err != nil {
			logger.Error(context.TODO(), err.Error())
			return true
		}

		mcl.Annotations["kubeconfig"] = kubeconfig

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

func (c *ClusterServiceProvider) getClusterKubeconfig(clusterClient *clustersmgmtv1.ClusterClient, cluster *clustersmgmtv1.Cluster) (string, error) {
	credentialsResp, err := clusterClient.Credentials().Get().Send()
	if err != nil {
		if credentialsResp != nil && credentialsResp.Status() == http.StatusForbidden {
			api, ok := cluster.GetAPI()
			if !ok {
				return "", fmt.Errorf("failed to get cluster api for cluster %q", cluster.ID())
			}

			// TODO need to save the cluster admin to a secret instead of in a cache
			clusterAdmin, ok := c.clusterAdmins[cluster.ID()]
			if !ok {
				clusterAdmin, err = createClusterAdmin(clusterClient)
				if err != nil {
					return "", err
				}

				c.clusterAdmins[cluster.ID()] = clusterAdmin
			}

			kubeconfig, err := buildKubeConfig(api.URL(), clusterAdmin)
			if err != nil {
				return "", err
			}

			return string(kubeconfig), nil
		}

		return "", err
	}

	return credentialsResp.Body().Kubeconfig(), nil
}

func createClusterAdmin(clusterClient *clustersmgmtv1.ClusterClient) (*clustersmgmtv1.HTPasswdIdentityProvider, error) {
	user, err := clustersmgmtv1.NewUser().ID("acm-bootstrap-admin").Build()
	if err != nil {
		return nil, err
	}

	pw, err := password.Generate(20, 10, 0, false, false)
	if err != nil {
		return nil, err
	}

	htPasswdUserBuilder := clustersmgmtv1.NewHTPasswdUser().Username("acm-bootstrap-admin").Password(pw)
	htPasswdUserListBuilder := clustersmgmtv1.NewHTPasswdUserList().Items(htPasswdUserBuilder)
	htPasswdUsers := clustersmgmtv1.NewHTPasswdIdentityProvider().Users(htPasswdUserListBuilder)
	htPasswdIDProvider, err := htPasswdUsers.Build()
	if err != nil {
		return nil, err
	}

	idProvider, err := clustersmgmtv1.NewIdentityProvider().
		Name("acm-bootstrap-admin").
		Type(clustersmgmtv1.IdentityProviderTypeHtpasswd).
		Htpasswd(htPasswdUsers).
		Build()
	if err != nil {
		return nil, err
	}

	if _, err := clusterClient.Groups().Group("cluster-admins").Users().Add().Body(user).Send(); err != nil {
		return nil, err
	}

	if _, err := clusterClient.IdentityProviders().Add().Body(idProvider).Send(); err != nil {
		return nil, err
	}

	return htPasswdIDProvider, nil
}

func buildKubeConfig(apiURL string, admin *clustersmgmtv1.HTPasswdIdentityProvider) ([]byte, error) {
	var username, password string
	admin.Users().Each(func(user *clustersmgmtv1.HTPasswdUser) bool {
		username = user.Username()
		password = user.Password()
		return true
	})

	token, err := tokenrequest.RequestTokenWithChallengeHandlers(&rest.Config{
		Host:     apiURL,
		Username: username,
		Password: password,
	})
	if err != nil {
		return nil, err
	}

	bootstrapKubeconfig := &clientcmdapi.Config{
		Clusters: map[string]*clientcmdapi.Cluster{"default-cluster": {
			Server:                apiURL,
			InsecureSkipTLSVerify: true,
		}},
		AuthInfos: map[string]*clientcmdapi.AuthInfo{"default-auth": {
			Token: string(token),
		}},
		Contexts: map[string]*clientcmdapi.Context{"default-context": {
			Cluster:   "default-cluster",
			AuthInfo:  "default-auth",
			Namespace: "default",
		}},
		CurrentContext: "default-context",
	}

	return runtime.Encode(clientcmdlatest.Codec, bootstrapKubeconfig)
}
