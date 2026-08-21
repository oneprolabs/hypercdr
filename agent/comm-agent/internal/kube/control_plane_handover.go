package kube

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
)

const (
	HandoverSecretName            = "hypercdr-agent-handover"
	HandoverStatusPrepared        = "prepared"
	HandoverStatusTargetConfirmed = "target-confirmed"
	HandoverStatusCommitted       = "committed"
)

type ControlPlaneHandoverInput struct {
	MigrationID, Namespace, PreviousEndpoint, PreviousClusterID, PreviousCredential string
	TargetEndpoint, TargetInstallToken                                              string
	RollbackDeadline                                                                time.Time
}

type ControlPlaneHandoverState struct {
	MigrationID, Status, PreviousEndpoint, PreviousClusterID, PreviousCredential string
	TargetEndpoint, TargetInstallToken                                           string
	RollbackDeadline                                                             time.Time
}

type ControlPlaneHandoverManager interface {
	Prepare(context.Context, ControlPlaneHandoverInput) error
	Confirm(context.Context, string, string) error
	Rollback(context.Context, string, string) error
	Commit(context.Context, string, string) error
	Cleanup(context.Context, string, string) error
	RollbackExpired(context.Context, string, time.Time) (bool, error)
}

type KubernetesControlPlaneHandoverManager struct{ client kubernetes.Interface }

func NewKubernetesControlPlaneHandoverManager(kubeconfigPath string) (*KubernetesControlPlaneHandoverManager, error) {
	cfg, err := BuildRESTConfig(kubeconfigPath)
	if err != nil {
		return nil, err
	}
	client, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}
	return &KubernetesControlPlaneHandoverManager{client: client}, nil
}

func NewKubernetesControlPlaneHandoverManagerWithClient(client kubernetes.Interface) *KubernetesControlPlaneHandoverManager {
	return &KubernetesControlPlaneHandoverManager{client: client}
}

func (m *KubernetesControlPlaneHandoverManager) Prepare(ctx context.Context, input ControlPlaneHandoverInput) error {
	if strings.TrimSpace(input.MigrationID) == "" {
		return errors.New("migration ID is required")
	}
	if input.Namespace == "" {
		input.Namespace = "hypercdr-agent"
	}
	if input.Namespace != "hypercdr-agent" {
		return errors.New("control-plane handover requires the canonical hypercdr-agent namespace")
	}
	if err := validateHandoverEndpoint(input.TargetEndpoint); err != nil {
		return err
	}
	if strings.TrimSpace(input.TargetInstallToken) == "" {
		return errors.New("target install token is required")
	}
	if input.RollbackDeadline.IsZero() || !input.RollbackDeadline.After(time.Now().UTC()) {
		return errors.New("rollback deadline must be in the future")
	}
	if _, err := m.client.AppsV1().Deployments(input.Namespace).Get(ctx, "hypercdr-comm-agent", metav1.GetOptions{}); err != nil {
		return fmt.Errorf("read comm-agent deployment: %w", err)
	}
	if existing, err := m.load(ctx, input.Namespace); err == nil && existing.MigrationID != input.MigrationID {
		return fmt.Errorf("handover %s is already active", existing.MigrationID)
	} else if err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	state := ControlPlaneHandoverState{
		MigrationID: input.MigrationID, Status: HandoverStatusPrepared,
		PreviousEndpoint: input.PreviousEndpoint, PreviousClusterID: input.PreviousClusterID,
		PreviousCredential: input.PreviousCredential, TargetEndpoint: input.TargetEndpoint,
		TargetInstallToken: input.TargetInstallToken, RollbackDeadline: input.RollbackDeadline.UTC(),
	}
	if err := m.save(ctx, input.Namespace, state); err != nil {
		return err
	}
	if err := m.writeBootstrap(ctx, input.Namespace, input.TargetEndpoint, input.TargetInstallToken); err != nil {
		return err
	}
	if err := m.client.CoreV1().Secrets(input.Namespace).Delete(ctx, "hypercdr-agent-credential", metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	return m.restart(ctx, input.Namespace, "prepare-"+input.MigrationID)
}

func (m *KubernetesControlPlaneHandoverManager) Confirm(ctx context.Context, namespace, migrationID string) error {
	state, err := m.requireState(ctx, namespace, migrationID)
	if err != nil {
		return err
	}
	state.Status = HandoverStatusTargetConfirmed
	return m.save(ctx, canonicalNamespace(namespace), state)
}

func (m *KubernetesControlPlaneHandoverManager) Rollback(ctx context.Context, namespace, migrationID string) error {
	namespace = canonicalNamespace(namespace)
	state, err := m.requireState(ctx, namespace, migrationID)
	if err != nil {
		return err
	}
	if err := m.writeBootstrap(ctx, namespace, state.PreviousEndpoint, ""); err != nil {
		return err
	}
	if err := m.writeCredential(ctx, namespace, state.PreviousClusterID, state.PreviousCredential); err != nil {
		return err
	}
	if err := m.client.CoreV1().Secrets(namespace).Delete(ctx, HandoverSecretName, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	return m.restart(ctx, namespace, "rollback-"+migrationID)
}

func (m *KubernetesControlPlaneHandoverManager) Commit(ctx context.Context, namespace, migrationID string) error {
	namespace = canonicalNamespace(namespace)
	state, err := m.requireState(ctx, namespace, migrationID)
	if err != nil {
		return err
	}
	if state.Status != HandoverStatusTargetConfirmed {
		if state.Status == HandoverStatusCommitted {
			return nil
		}
		return fmt.Errorf("handover is %s, not target-confirmed", state.Status)
	}
	// Persist the irreversible decision before acknowledging it.  The deadline
	// watcher must never move an Agent back to Community after the target has
	// durably decided to commit.
	state.Status = HandoverStatusCommitted
	return m.save(ctx, namespace, state)
}

// Cleanup removes rollback material only after both control planes have
// durably completed Commit. It is deliberately separate from Commit so a
// coordinator crash cannot make an Agent auto-rollback a committed migration.
func (m *KubernetesControlPlaneHandoverManager) Cleanup(ctx context.Context, namespace, migrationID string) error {
	namespace = canonicalNamespace(namespace)
	state, err := m.requireState(ctx, namespace, migrationID)
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if state.Status != HandoverStatusCommitted {
		return fmt.Errorf("handover is %s, not committed", state.Status)
	}
	err = m.client.CoreV1().Secrets(namespace).Delete(ctx, HandoverSecretName, metav1.DeleteOptions{})
	if apierrors.IsNotFound(err) {
		return nil
	}
	return err
}

func (m *KubernetesControlPlaneHandoverManager) RollbackExpired(ctx context.Context, namespace string, now time.Time) (bool, error) {
	namespace = canonicalNamespace(namespace)
	state, err := m.load(ctx, namespace)
	if apierrors.IsNotFound(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if now.UTC().Before(state.RollbackDeadline) {
		return false, nil
	}
	if state.Status == HandoverStatusCommitted {
		return false, nil
	}
	return true, m.Rollback(ctx, namespace, state.MigrationID)
}

func (m *KubernetesControlPlaneHandoverManager) requireState(ctx context.Context, namespace, migrationID string) (ControlPlaneHandoverState, error) {
	state, err := m.load(ctx, canonicalNamespace(namespace))
	if err != nil {
		return state, err
	}
	if state.MigrationID != migrationID {
		return state, fmt.Errorf("active handover %s does not match %s", state.MigrationID, migrationID)
	}
	return state, nil
}

func (m *KubernetesControlPlaneHandoverManager) load(ctx context.Context, namespace string) (ControlPlaneHandoverState, error) {
	secret, err := m.client.CoreV1().Secrets(namespace).Get(ctx, HandoverSecretName, metav1.GetOptions{})
	if err != nil {
		return ControlPlaneHandoverState{}, err
	}
	var state ControlPlaneHandoverState
	if err := json.Unmarshal(secret.Data["state.json"], &state); err != nil {
		return state, fmt.Errorf("decode handover state: %w", err)
	}
	return state, nil
}

func (m *KubernetesControlPlaneHandoverManager) save(ctx context.Context, namespace string, state ControlPlaneHandoverState) error {
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	secrets := m.client.CoreV1().Secrets(namespace)
	secret, err := secrets.Get(ctx, HandoverSecretName, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		_, err = secrets.Create(ctx, &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: HandoverSecretName, Namespace: namespace, Labels: map[string]string{"app.kubernetes.io/name": "hypercdr-comm-agent", "hypercdr.io/purpose": "control-plane-handover"}}, Type: corev1.SecretTypeOpaque, Data: map[string][]byte{"state.json": data}}, metav1.CreateOptions{})
		return err
	}
	if err != nil {
		return err
	}
	secret.Data = map[string][]byte{"state.json": data}
	_, err = secrets.Update(ctx, secret, metav1.UpdateOptions{})
	return err
}

func (m *KubernetesControlPlaneHandoverManager) writeBootstrap(ctx context.Context, namespace, endpoint, token string) error {
	secrets := m.client.CoreV1().Secrets(namespace)
	secret, err := secrets.Get(ctx, "hypercdr-agent-bootstrap", metav1.GetOptions{})
	if err != nil {
		return err
	}
	if secret.Data == nil {
		secret.Data = map[string][]byte{}
	}
	secret.Data["HCDR_PLATFORM_ENDPOINT"] = []byte(endpoint)
	secret.Data["HCDR_INSTALL_TOKEN"] = []byte(token)
	_, err = secrets.Update(ctx, secret, metav1.UpdateOptions{})
	return err
}

func (m *KubernetesControlPlaneHandoverManager) writeCredential(ctx context.Context, namespace, clusterID, credential string) error {
	return NewSecretCredentialStoreWithClient(m.client, namespace, "hypercdr-agent-credential").Save(ctx, AgentCredential{ClusterID: clusterID, Credential: credential})
}

func (m *KubernetesControlPlaneHandoverManager) restart(ctx context.Context, namespace, reason string) error {
	patch, _ := json.Marshal(map[string]any{"spec": map[string]any{"template": map[string]any{"metadata": map[string]any{"annotations": map[string]string{"hypercdr.io/control-plane-handover-at": time.Now().UTC().Format(time.RFC3339Nano), "hypercdr.io/control-plane-handover": reason}}}}})
	_, err := m.client.AppsV1().Deployments(namespace).Patch(ctx, "hypercdr-comm-agent", types.StrategicMergePatchType, patch, metav1.PatchOptions{})
	return err
}

func validateHandoverEndpoint(raw string) error {
	target, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || target.Host == "" || (target.Scheme != "ws" && target.Scheme != "wss") {
		return errors.New("target endpoint must be a ws or wss URL")
	}
	return nil
}

func canonicalNamespace(namespace string) string {
	if namespace == "" {
		return "hypercdr-agent"
	}
	return namespace
}
