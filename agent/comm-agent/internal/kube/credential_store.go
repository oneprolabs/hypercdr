package kube

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	CredentialKeyClusterID = "HCDR_CLUSTER_ID"
	CredentialKeySecret    = "HCDR_AGENT_CREDENTIAL"
)

type AgentCredential struct {
	ClusterID  string
	Credential string
}

type CredentialStore interface {
	Load(ctx context.Context) (AgentCredential, bool, error)
	Save(ctx context.Context, credential AgentCredential) error
}

type SecretCredentialStore struct {
	client    kubernetes.Interface
	namespace string
	name      string
}

func NewSecretCredentialStore(kubeconfigPath string, namespace string, name string) (*SecretCredentialStore, error) {
	cfg, err := BuildRESTConfig(kubeconfigPath)
	if err != nil {
		return nil, err
	}
	client, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}
	return NewSecretCredentialStoreWithClient(client, namespace, name), nil
}

func NewSecretCredentialStoreWithClient(client kubernetes.Interface, namespace string, name string) *SecretCredentialStore {
	return &SecretCredentialStore{client: client, namespace: namespace, name: name}
}

func (s *SecretCredentialStore) Load(ctx context.Context) (AgentCredential, bool, error) {
	secret, err := s.client.CoreV1().Secrets(s.namespace).Get(ctx, s.name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return AgentCredential{}, false, nil
	}
	if err != nil {
		return AgentCredential{}, false, err
	}
	clusterID := string(secret.Data[CredentialKeyClusterID])
	credential := string(secret.Data[CredentialKeySecret])
	if clusterID == "" || credential == "" {
		return AgentCredential{}, false, nil
	}
	return AgentCredential{ClusterID: clusterID, Credential: credential}, true, nil
}

func (s *SecretCredentialStore) Save(ctx context.Context, credential AgentCredential) error {
	data := map[string][]byte{
		CredentialKeyClusterID: []byte(credential.ClusterID),
		CredentialKeySecret:    []byte(credential.Credential),
	}
	secret, err := s.client.CoreV1().Secrets(s.namespace).Get(ctx, s.name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		_, err := s.client.CoreV1().Secrets(s.namespace).Create(ctx, &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      s.name,
				Namespace: s.namespace,
				Labels: map[string]string{
					"app.kubernetes.io/name": "hypercdr-comm-agent",
					"hypercdr.io/managed-by": "comm-agent",
				},
			},
			Type: corev1.SecretTypeOpaque,
			Data: data,
		}, metav1.CreateOptions{})
		return err
	}
	if err != nil {
		return err
	}
	if secret.Data == nil {
		secret.Data = map[string][]byte{}
	}
	for key, value := range data {
		secret.Data[key] = value
	}
	_, err = s.client.CoreV1().Secrets(s.namespace).Update(ctx, secret, metav1.UpdateOptions{})
	return err
}
