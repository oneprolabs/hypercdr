package controller

import (
	"context"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

const storageAccessPolicy = "hypercdr.io/storage-access"

func onDemandStorage(object client.Object) bool {
	return object.GetAnnotations()[storageAccessPolicy] == "on-demand-v1"
}

func storageRequestPending(object client.Object, operation string) bool {
	annotations := object.GetAnnotations()
	requested := annotations["hypercdr.io/"+operation+"-request"]
	if requested == "" || requested == annotations["hypercdr.io/"+operation+"-completed"] {
		return false
	}
	when, err := time.Parse(time.RFC3339Nano, requested)
	return err == nil && time.Since(when) >= -time.Minute && time.Since(when) < 5*time.Minute
}

func completeStorageRequest(ctx context.Context, cli client.Client, object client.Object, operation string, succeeded bool) error {
	requested := object.GetAnnotations()["hypercdr.io/"+operation+"-request"]
	latest := object.DeepCopyObject().(client.Object)
	if err := cli.Get(ctx, client.ObjectKeyFromObject(object), latest); err != nil {
		return err
	}
	before := latest.DeepCopyObject().(client.Object)
	annotations := latest.GetAnnotations()
	if annotations == nil {
		annotations = map[string]string{}
	}
	annotations["hypercdr.io/"+operation+"-completed"] = requested
	annotations["hypercdr.io/"+operation+"-result"] = "failed"
	if succeeded {
		annotations["hypercdr.io/"+operation+"-result"] = "succeeded"
	}
	latest.SetAnnotations(annotations)
	return cli.Patch(ctx, latest, client.MergeFrom(before))
}
