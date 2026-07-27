package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

func main() {
	kubeconfig := flag.String("kubeconfig", "", "path to kubeconfig")
	namespace := flag.String("namespace", "hypercdr-agent", "namespace to inspect")
	flag.Parse()
	if *kubeconfig == "" {
		fmt.Fprintln(os.Stderr, "--kubeconfig is required")
		os.Exit(2)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cfg, err := clientcmd.BuildConfigFromFlags("", *kubeconfig)
	if err != nil {
		fail("load kubeconfig", err)
	}
	client, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		fail("create client", err)
	}

	version, err := client.Discovery().ServerVersion()
	if err != nil {
		fail("query server version", err)
	}
	fmt.Printf("cluster server version: %s\n\n", version.String())

	printNodes(ctx, client)
	printDeployments(ctx, client, *namespace)
	pods := printPods(ctx, client, *namespace)
	printEvents(ctx, client, *namespace)
	printSelectedLogs(ctx, client, *namespace, pods)
}

func printNodes(ctx context.Context, client kubernetes.Interface) {
	fmt.Println("== nodes ==")
	list, err := client.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		fmt.Printf("list nodes failed: %v\n\n", err)
		return
	}
	if len(list.Items) == 0 {
		fmt.Println("(none)")
	}
	for _, node := range list.Items {
		fmt.Printf("%s kubelet=%s runtime=%s os=%s arch=%s\n",
			node.Name,
			node.Status.NodeInfo.KubeletVersion,
			node.Status.NodeInfo.ContainerRuntimeVersion,
			node.Status.NodeInfo.OSImage,
			node.Status.NodeInfo.Architecture,
		)
	}
	fmt.Println()
}

func printDeployments(ctx context.Context, client kubernetes.Interface, namespace string) {
	fmt.Println("== deployments ==")
	list, err := client.AppsV1().Deployments(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		fmt.Printf("list deployments failed: %v\n\n", err)
		return
	}
	if len(list.Items) == 0 {
		fmt.Println("(none)")
	}
	for _, item := range list.Items {
		fmt.Printf("%s desired=%d available=%d updated=%d ready=%d\n",
			item.Name,
			ptrInt32(item.Spec.Replicas),
			item.Status.AvailableReplicas,
			item.Status.UpdatedReplicas,
			item.Status.ReadyReplicas,
		)
		for _, condition := range item.Status.Conditions {
			fmt.Printf("  condition %s=%s reason=%s message=%s\n", condition.Type, condition.Status, condition.Reason, condition.Message)
		}
	}
	fmt.Println()
}

func printPods(ctx context.Context, client kubernetes.Interface, namespace string) []corev1.Pod {
	fmt.Println("== pods ==")
	list, err := client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		fmt.Printf("list pods failed: %v\n\n", err)
		return nil
	}
	sort.Slice(list.Items, func(i, j int) bool { return list.Items[i].Name < list.Items[j].Name })
	if len(list.Items) == 0 {
		fmt.Println("(none)")
	}
	for _, pod := range list.Items {
		fmt.Printf("%s phase=%s node=%s podIP=%s\n", pod.Name, pod.Status.Phase, pod.Spec.NodeName, pod.Status.PodIP)
		for _, condition := range pod.Status.Conditions {
			if condition.Status != corev1.ConditionTrue {
				fmt.Printf("  condition %s=%s reason=%s message=%s\n", condition.Type, condition.Status, condition.Reason, condition.Message)
			}
		}
		for _, status := range pod.Status.ContainerStatuses {
			fmt.Printf("  container %s ready=%t restart=%d image=%s\n", status.Name, status.Ready, status.RestartCount, status.Image)
			if status.State.Waiting != nil {
				fmt.Printf("    waiting reason=%s message=%s\n", status.State.Waiting.Reason, status.State.Waiting.Message)
			}
			if status.State.Terminated != nil {
				fmt.Printf("    terminated reason=%s exit=%d message=%s\n", status.State.Terminated.Reason, status.State.Terminated.ExitCode, status.State.Terminated.Message)
			}
		}
	}
	fmt.Println()
	return list.Items
}

func printEvents(ctx context.Context, client kubernetes.Interface, namespace string) {
	fmt.Println("== recent events ==")
	list, err := client.CoreV1().Events(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		fmt.Printf("list events failed: %v\n\n", err)
		return
	}
	sort.Slice(list.Items, func(i, j int) bool {
		return list.Items[i].LastTimestamp.Time.Before(list.Items[j].LastTimestamp.Time)
	})
	start := 0
	if len(list.Items) > 30 {
		start = len(list.Items) - 30
	}
	if len(list.Items) == 0 {
		fmt.Println("(none)")
	}
	for _, event := range list.Items[start:] {
		fmt.Printf("%s %s/%s %s %s: %s\n",
			event.LastTimestamp.Format(time.RFC3339),
			event.InvolvedObject.Kind,
			event.InvolvedObject.Name,
			event.Type,
			event.Reason,
			event.Message,
		)
	}
	fmt.Println()
}

func printSelectedLogs(ctx context.Context, client kubernetes.Interface, namespace string, pods []corev1.Pod) {
	fmt.Println("== selected logs ==")
	found := false
	for _, pod := range pods {
		if !strings.Contains(pod.Name, "hypercdr-comm-agent") && !strings.Contains(pod.Name, "velero") {
			continue
		}
		found = true
		for _, container := range pod.Spec.Containers {
			fmt.Printf("-- %s/%s --\n", pod.Name, container.Name)
			tail := int64(120)
			req := client.CoreV1().Pods(namespace).GetLogs(pod.Name, &corev1.PodLogOptions{
				Container: container.Name,
				TailLines: &tail,
			})
			stream, err := req.Stream(ctx)
			if err != nil {
				fmt.Printf("logs failed: %v\n", err)
				continue
			}
			_, _ = io.Copy(os.Stdout, stream)
			_ = stream.Close()
			fmt.Println()
		}
	}
	if !found {
		fmt.Println("(no comm-agent or velero pods found)")
	}
}

func ptrInt32(value *int32) int32 {
	if value == nil {
		return 0
	}
	return *value
}

func fail(action string, err error) {
	fmt.Fprintf(os.Stderr, "%s failed: %v\n", action, err)
	os.Exit(1)
}
