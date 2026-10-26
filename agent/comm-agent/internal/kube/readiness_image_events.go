package kube

import (
	"sort"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

type ReadinessImageEvent struct {
	ID          string
	PodUID      string
	Pod         string
	DisplayName string
	Container   string
	Image       string
	Node        string
	Reason      string
	Message     string
	FirstAt     time.Time
	LastAt      time.Time
	Count       int64
}

func readinessImageEvents(objects []ReadinessObject, events []unstructured.Unstructured) []ReadinessImageEvent {
	pods := map[string]ReadinessObject{}
	for _, object := range objects {
		if object.Kind == "Pod" && object.UID != "" {
			pods[object.UID] = object
		}
	}
	result := []ReadinessImageEvent{}
	for _, event := range events {
		uid, _, _ := unstructured.NestedString(event.Object, "involvedObject", "uid")
		pod, found := pods[uid]
		if !found {
			continue
		}
		reason, _, _ := unstructured.NestedString(event.Object, "reason")
		message, _, _ := unstructured.NestedString(event.Object, "message")
		lower := strings.ToLower(message)
		if reason != "Pulling" && reason != "Pulled" && !((reason == "Failed" || reason == "BackOff") && (strings.Contains(lower, "image") || strings.Contains(lower, "errimagepull"))) {
			continue
		}
		fieldPath, _, _ := unstructured.NestedString(event.Object, "involvedObject", "fieldPath")
		container := ""
		if start := strings.Index(fieldPath, "{"); start >= 0 && strings.HasSuffix(fieldPath, "}") {
			container = fieldPath[start+1 : len(fieldPath)-1]
		}
		image := pod.Images[container]
		if image == "" {
			if container != "" {
				continue
			}
			matches := 0
			for name, candidate := range pod.Images {
				if candidate != "" && strings.Contains(message, "\""+candidate+"\"") {
					container, image = name, candidate
					matches++
				}
			}
			if matches != 1 {
				continue
			}
		}
		if image == "" || container == "restore-wait" {
			continue
		}
		if quoted := strings.Split(message, "\""); len(quoted) >= 3 && strings.Contains(strings.ToLower(quoted[0]), "image") {
			image = quoted[1]
		}
		if reason == "Pulled" && strings.Contains(lower, "already present") {
			reason = "ImageCached"
		}
		first, ok := nestedTime(event.Object, "firstTimestamp")
		if !ok {
			first, ok = nestedTime(event.Object, "eventTime")
		}
		if !ok {
			first = event.GetCreationTimestamp().Time
		}
		last, ok := nestedTime(event.Object, "lastTimestamp")
		if !ok {
			last = first
		}
		if observed, ok := nestedTime(event.Object, "series", "lastObservedTime"); ok && observed.After(last) {
			last = observed
		}
		count := int64FieldPath(event.Object, "count")
		if seriesCount := int64FieldPath(event.Object, "series", "count"); seriesCount > count {
			count = seriesCount
		}
		if count < 1 {
			count = 1
		}
		result = append(result, ReadinessImageEvent{ID: string(event.GetUID()), PodUID: uid, Pod: pod.Name, DisplayName: pod.DisplayName, Container: container, Image: image, Node: pod.Node, Reason: reason, Message: message, FirstAt: first, LastAt: last, Count: count})
	}
	sort.Slice(result, func(left, right int) bool {
		if result[left].FirstAt.Equal(result[right].FirstAt) {
			return result[left].ID < result[right].ID
		}
		return result[left].FirstAt.Before(result[right].FirstAt)
	})
	return result
}
