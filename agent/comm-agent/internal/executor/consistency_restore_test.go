package executor

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"hypercdr-platform/agent/comm-agent/internal/kube"
	"hypercdr-platform/agent/comm-agent/internal/velero"
	"hypercdr-platform/agent/comm-agent/pkg/protocol"
)

type consistencyRestoreMutationGuard struct {
	mutations int
}

func (guard *consistencyRestoreMutationGuard) FindConsistencyRestore(context.Context, kube.Manifest) (bool, error) {
	return false, nil
}

func (guard *consistencyRestoreMutationGuard) CreateConsistencyRestore(context.Context, kube.Manifest) error {
	guard.mutations++
	return nil
}

func (guard *consistencyRestoreMutationGuard) ApplyManifest(context.Context, kube.Manifest) (kube.AppliedObject, error) {
	guard.mutations++
	return kube.AppliedObject{}, nil
}

func (guard *consistencyRestoreMutationGuard) WaitForVeleroBackup(context.Context, string, string, time.Duration) error {
	return nil
}

func (guard *consistencyRestoreMutationGuard) ReplaceNamespaceAndWait(context.Context, string) error {
	guard.mutations++
	return nil
}

type rejectingConsistencyRestoreVerifier struct {
	*consistencyRestoreMutationGuard
	checks int
}

func (verifier *rejectingConsistencyRestoreVerifier) VerifyConsistencyRestoreBackup(context.Context, string, protocol.ConsistencyRestoreEvidence) error {
	verifier.checks++
	return errors.New("backup evidence mismatch")
}

func TestConsistencyRestoreFailsBeforeNamespaceReplacementOrApply(t *testing.T) {
	for _, scenario := range []string{"missing-verifier", "mismatched-backup"} {
		t.Run(scenario, func(t *testing.T) {
			guard := &consistencyRestoreMutationGuard{}
			var applier kube.ManifestApplier = guard
			verifier := &rejectingConsistencyRestoreVerifier{consistencyRestoreMutationGuard: guard}
			if scenario == "mismatched-backup" {
				applier = verifier
			}
			manifest := velero.RestoreManifest{Metadata: velero.ManifestMetadata{Namespace: "agent", Labels: map[string]string{"hypercdr.io/conflict-policy": "replace"}}, Spec: velero.RestoreManifestSpec{BackupName: "backup", IncludedNamespaces: []string{"app"}}, ConsistencyEvidence: &protocol.ConsistencyRestoreEvidence{}}
			err := (KubernetesRestoreExecutor{agentNamespace: "agent", applier: applier}).SubmitRestore(context.Background(), manifest)
			if err == nil || !strings.Contains(err.Error(), "consistency backup verification") || guard.mutations != 0 {
				t.Fatalf("unverified restore mutated destination or bypassed verification: %v, mutations=%d", err, guard.mutations)
			}
			if scenario == "mismatched-backup" && verifier.checks != 1 {
				t.Fatal("destination verifier was not called")
			}
		})
	}
}
