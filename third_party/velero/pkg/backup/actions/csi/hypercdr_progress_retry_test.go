package csi

import (
	"context"
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	crclient "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/vmware-tanzu/velero/pkg/builder"
	velerotest "github.com/vmware-tanzu/velero/pkg/test"
)

type progressReadErrorClient struct {
	crclient.Client
	err error
}

func (client progressReadErrorClient) List(context.Context, crclient.ObjectList, ...crclient.ListOption) error {
	return client.err
}

func TestProgressRetriesTransientAPIReadFailures(t *testing.T) {
	for _, readError := range []error{
		apierrors.NewTimeoutError("API timeout", 1),
		apierrors.NewServerTimeout(schema.GroupResource{Group: "velero.io", Resource: "datauploads"}, "list", 1),
		apierrors.NewTooManyRequests("API busy", 1),
		apierrors.NewServiceUnavailable("API unavailable"),
	} {
		action := pvcBackupItemAction{log: logrus.New(), crClient: progressReadErrorClient{Client: velerotest.NewFakeControllerRuntimeClient(t), err: readError}}
		progress, err := action.Progress("operation", builder.ForBackup("velero", "backup").Result())
		require.NoError(t, err)
		require.False(t, progress.Completed)
		require.Empty(t, progress.Err)
		require.Contains(t, progress.Description, "Kubernetes API")
	}
	action := pvcBackupItemAction{log: logrus.New(), crClient: progressReadErrorClient{err: apierrors.NewForbidden(schema.GroupResource{Resource: "datauploads"}, "", nil)}}
	_, err := action.Progress("operation", builder.ForBackup("velero", "backup").Result())
	require.Error(t, err)
}
