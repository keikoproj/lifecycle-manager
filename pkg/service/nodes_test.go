package service

import (
	"context"
	"reflect"
	"testing"

	v1 "k8s.io/api/core/v1"
	apimachinery_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
		kubeClient.CoreV1().Nodes().Create(context.Background(), &node, apimachinery_v1.CreateOptions{})
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
		kubeClient.CoreV1().Nodes().Create(context.Background(), &node, apimachinery_v1.CreateOptions{})
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
		kubeClient.CoreV1().Nodes().Create(context.Background(), &node, apimachinery_v1.CreateOptions{})
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
	kubeClient.CoreV1().Nodes().Create(context.Background(), readyNode, apimachinery_v1.CreateOptions{})
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
