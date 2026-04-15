package service

import (
	"context"
	"reflect"
	"testing"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

var (
	stubKubectlPathSuccess = "echo"
	stubKubectlPathFail    = "/bin/some-bad-file"
)

func Test_NodeStatusPredicate(t *testing.T) {
	t.Log("Test_NodeStatusPredicate: should return true if node readiness is in given condition")

	readyNode := v1.Node{
		Status: v1.NodeStatus{
			Conditions: []v1.NodeCondition{
				{
					Type:   v1.NodeReady,
					Status: v1.ConditionTrue,
				},
			},
		},
	}

	unknownNode := v1.Node{
		Status: v1.NodeStatus{
			Conditions: []v1.NodeCondition{
				{
					Type:   v1.NodeReady,
					Status: v1.ConditionUnknown,
				},
			},
		},
	}

	if isNodeStatusInCondition(readyNode, v1.ConditionTrue) != true {
		t.Fatalf("expected isNodeStatusInCondition exists to be: %t, got: %t", true, false)
	}

	if isNodeStatusInCondition(unknownNode, v1.ConditionUnknown) != true {
		t.Fatalf("expected isNodeStatusInCondition exists to be: %t, got: %t", true, false)
	}

}

func Test_GetNodeByInstancePositive(t *testing.T) {
	t.Log("Test_GetNodeByInstancePositive: If a node exists, should be able to get it's instance ID")
	kubeClient := fake.NewSimpleClientset()
	fakeNodes := []v1.Node{
		{
			Spec: v1.NodeSpec{
				ProviderID: "aws:///us-west-2a/i-11111111111111111",
			},
		},
		{
			Spec: v1.NodeSpec{
				ProviderID: "aws:///us-west-2c/i-22222222222222222",
			},
		},
	}

	for _, node := range fakeNodes {
		kubeClient.CoreV1().Nodes().Create(context.Background(), &node, metav1.CreateOptions{})
	}

	_, exists := getNodeByInstance(kubeClient, "i-11111111111111111")
	expected := true

	if exists != expected {
		t.Fatalf("expected getNodeByInstance exists to be: %v, got: %v", expected, exists)
	}
}

func Test_GetNodeByInstanceNegative(t *testing.T) {
	t.Log("Test_GetNodeByInstanceNegative: If a node exists, should be able to get it's instance ID")
	kubeClient := fake.NewSimpleClientset()
	fakeNodes := []v1.Node{
		{
			Spec: v1.NodeSpec{
				ProviderID: "aws:///us-west-2a/i-11111111111111111",
			},
		},
		{
			Spec: v1.NodeSpec{
				ProviderID: "aws:///us-west-2c/i-22222222222222222",
			},
		},
	}

	for _, node := range fakeNodes {
		kubeClient.CoreV1().Nodes().Create(context.Background(), &node, metav1.CreateOptions{})
	}

	_, exists := getNodeByInstance(kubeClient, "i-3333333333333333")
	expected := false

	if exists != expected {
		t.Fatalf("expected getNodeByInstance exists to be: %v, got: %v", expected, exists)
	}
}

func Test_GetNodesByAnnotationKey(t *testing.T) {
	t.Log("Test_GetNodesByAnnotationKey: Get map of nodes annotation values by a key")
	kubeClient := fake.NewSimpleClientset()
	fakeNodes := []v1.Node{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: "node-1",
				Annotations: map[string]string{
					"some-key":    "some-value",
					"another-key": "another-value",
				},
			},
			Spec: v1.NodeSpec{
				ProviderID: "aws:///us-west-2a/i-11111111111111111",
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: "node-2",
				Annotations: map[string]string{
					"some-other-key":    "some-value",
					"another-other-key": "another-value",
				},
			},
			Spec: v1.NodeSpec{
				ProviderID: "aws:///us-west-2c/i-22222222222222222",
			},
		},
	}

	for _, node := range fakeNodes {
		kubeClient.CoreV1().Nodes().Create(context.Background(), &node, metav1.CreateOptions{})
	}

	result, err := getNodesByAnnotationKeys(kubeClient, "some-key", "another-key")
	expected := map[string]map[string]string{
		"node-1": {
			"some-key":    "some-value",
			"another-key": "another-value",
		},
	}

	if err != nil {
		t.Fatalf("getNodesByAnnotationKey: expected error not to have occured, %v", err)
	}

	if !reflect.DeepEqual(result, expected) {
		t.Fatalf("getNodesByAnnotationKey: expected: %v, got: %v", expected, result)
	}
}

func Test_DrainNodePositive(t *testing.T) {
	t.Log("Test_DrainNodePositive: If drain process is successful, process should exit successfully")
	kubeClient := fake.NewSimpleClientset()
	readyNode := &v1.Node{
		Status: v1.NodeStatus{
			Conditions: []v1.NodeCondition{
				{
					Type:   v1.NodeReady,
					Status: v1.ConditionTrue,
				},
			},
		},
	}
	kubeClient.CoreV1().Nodes().Create(context.Background(), readyNode, metav1.CreateOptions{})
	err := drainNode(kubeClient, readyNode, 10, 0, 3)
	if err != nil {
		t.Fatalf("drainNode: expected error not to have occured, %v", err)
	}
}

func Test_DrainNodeNegative(t *testing.T) {
	t.Log("Test_DrainNodeNegative: node is not part of cluster, drainNode should return error")
	kubeClient := fake.NewSimpleClientset()
	unjoinedNode := &v1.Node{
		Status: v1.NodeStatus{
			Conditions: []v1.NodeCondition{
				{
					Type:   v1.NodeReady,
					Status: v1.ConditionUnknown,
				},
			},
		},
	}

	err := drainNode(kubeClient, unjoinedNode, 10, 30, 3)
	if err == nil {
		t.Fatalf("drainNode: expected error to have occured, %v", err)
	}
}

func Test_RunCommand(t *testing.T) {
	t.Log("Test_DrainNodePositive: should successfully run command")
	_, err := runCommand("/bin/sleep", []string{"0"})
	if err != nil {
		t.Fatalf("drainNode: expected error not to have occured, %v", err)
	}
}

func Test_LabelNodePositive(t *testing.T) {
	t.Log("Test_LabelNode: should not return an error if succesful")
	var (
		nodeName = "some-node"
	)

	err := labelNode(stubKubectlPathSuccess, nodeName, ExcludeLabelKey, ExcludeLabelValue)
	if err != nil {
		t.Fatalf("Test_LabelNode: expected error not to have occured, %v", err)
	}
}

func Test_LabelNodeNegative(t *testing.T) {
	t.Log("Test_LabelNode: should return an error if succesful")
	var (
		nodeName = "some-node"
	)

	err := labelNode(stubKubectlPathFail, nodeName, ExcludeLabelKey, ExcludeLabelValue)
	if err == nil {
		t.Fatalf("Test_LabelNode: expected error to have occured, %v", err)
	}
}

func Test_IsDaemonSetPod(t *testing.T) {
	t.Log("Test_IsDaemonSetPod: should correctly identify daemonset pods")

	dsPod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			OwnerReferences: []metav1.OwnerReference{
				{Kind: "DaemonSet", Name: "my-ds", APIVersion: "apps/v1"},
			},
		},
	}
	if !isDaemonSetPod(dsPod) {
		t.Fatal("expected daemonset pod to be identified as such")
	}

	regularPod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			OwnerReferences: []metav1.OwnerReference{
				{Kind: "ReplicaSet", Name: "my-rs", APIVersion: "apps/v1"},
			},
		},
	}
	if isDaemonSetPod(regularPod) {
		t.Fatal("expected regular pod not to be identified as daemonset pod")
	}

	noPod := &v1.Pod{}
	if isDaemonSetPod(noPod) {
		t.Fatal("expected pod with no owner references not to be identified as daemonset pod")
	}
}

func Test_DeletePodsOnNode(t *testing.T) {
	t.Log("Test_DeletePodsOnNode: should delete non-daemonset pods and skip daemonset pods")
	kubeClient := fake.NewSimpleClientset()

	node := &v1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "test-node"},
	}
	kubeClient.CoreV1().Nodes().Create(context.Background(), node, metav1.CreateOptions{})

	regularPod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "regular-pod", Namespace: "default"},
		Spec:       v1.PodSpec{NodeName: "test-node"},
		Status:     v1.PodStatus{Phase: v1.PodRunning},
	}
	kubeClient.CoreV1().Pods("default").Create(context.Background(), regularPod, metav1.CreateOptions{})

	dsPod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ds-pod",
			Namespace: "kube-system",
			OwnerReferences: []metav1.OwnerReference{
				{Kind: "DaemonSet", Name: "my-ds", APIVersion: "apps/v1"},
			},
		},
		Spec:   v1.PodSpec{NodeName: "test-node"},
		Status: v1.PodStatus{Phase: v1.PodRunning},
	}
	kubeClient.CoreV1().Pods("kube-system").Create(context.Background(), dsPod, metav1.CreateOptions{})

	deleted, err := deletePodsOnNode(kubeClient, "test-node", 900)
	if err != nil {
		t.Fatalf("deletePodsOnNode: expected no error, got: %v", err)
	}

	if deleted != 1 {
		t.Fatalf("expected 1 pod deleted, got: %d", deleted)
	}

	remaining, _ := kubeClient.CoreV1().Pods("kube-system").List(context.Background(), metav1.ListOptions{})
	if len(remaining.Items) != 1 {
		t.Fatalf("expected daemonset pod to remain, got %d pods", len(remaining.Items))
	}
}

func Test_DeletePodsOnNodeCapsGracePeriod(t *testing.T) {
	t.Log("Test_DeletePodsOnNodeCapsGracePeriod: pod grace period should be capped to maxTerminationGracePeriod")
	kubeClient := fake.NewSimpleClientset()

	longGrace := int64(600)
	shortGrace := int64(30)

	longGracePod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "long-grace-pod", Namespace: "default"},
		Spec:       v1.PodSpec{NodeName: "test-node", TerminationGracePeriodSeconds: &longGrace},
		Status:     v1.PodStatus{Phase: v1.PodRunning},
	}
	shortGracePod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "short-grace-pod", Namespace: "default"},
		Spec:       v1.PodSpec{NodeName: "test-node", TerminationGracePeriodSeconds: &shortGrace},
		Status:     v1.PodStatus{Phase: v1.PodRunning},
	}
	kubeClient.CoreV1().Pods("default").Create(context.Background(), longGracePod, metav1.CreateOptions{})
	kubeClient.CoreV1().Pods("default").Create(context.Background(), shortGracePod, metav1.CreateOptions{})

	deleted, err := deletePodsOnNode(kubeClient, "test-node", 180)
	if err != nil {
		t.Fatalf("deletePodsOnNode: expected no error, got: %v", err)
	}

	if deleted != 2 {
		t.Fatalf("expected 2 pods deleted, got: %d", deleted)
	}

	remaining, _ := kubeClient.CoreV1().Pods("default").List(context.Background(), metav1.ListOptions{})
	if len(remaining.Items) != 0 {
		t.Fatalf("expected all pods deleted, but %d remain", len(remaining.Items))
	}
}

func Test_CountActivePods(t *testing.T) {
	t.Log("Test_CountActivePods: should count only non-daemonset, active pods")
	kubeClient := fake.NewSimpleClientset()

	pods := []*v1.Pod{
		{
			ObjectMeta: metav1.ObjectMeta{Name: "running-pod", Namespace: "default"},
			Spec:       v1.PodSpec{NodeName: "test-node"},
			Status:     v1.PodStatus{Phase: v1.PodRunning},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: "ds-pod", Namespace: "kube-system",
				OwnerReferences: []metav1.OwnerReference{{Kind: "DaemonSet", Name: "my-ds", APIVersion: "apps/v1"}},
			},
			Spec:   v1.PodSpec{NodeName: "test-node"},
			Status: v1.PodStatus{Phase: v1.PodRunning},
		},
		{
			ObjectMeta: metav1.ObjectMeta{Name: "succeeded-pod", Namespace: "default"},
			Spec:       v1.PodSpec{NodeName: "test-node"},
			Status:     v1.PodStatus{Phase: v1.PodSucceeded},
		},
	}
	for _, pod := range pods {
		kubeClient.CoreV1().Pods(pod.Namespace).Create(context.Background(), pod, metav1.CreateOptions{})
	}

	count := countActivePods(kubeClient, "test-node")
	if count != 1 {
		t.Fatalf("expected 1 active pod, got: %d", count)
	}
}
