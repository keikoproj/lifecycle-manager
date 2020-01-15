package service

import (
	"testing"

	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes/fake"
)

var (
	stubKubectlPathSuccess = "echo"
	stubKubectlPathFail    = "/bin/some-bad-file"
)

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
		kubeClient.CoreV1().Nodes().Create(&node)
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
		kubeClient.CoreV1().Nodes().Create(&node)
	}

	_, exists := getNodeByInstance(kubeClient, "i-3333333333333333")
	expected := false

	if exists != expected {
		t.Fatalf("expected getNodeByInstance exists to be: %v, got: %v", expected, exists)
	}
}

func Test_DrainNodePositive(t *testing.T) {
	t.Log("Test_DrainNodePositive: If drain process is successful, process should exit successfully")
	err := drainNode(stubKubectlPathSuccess, "some-node", 10, 0)
	if err != nil {
		t.Fatalf("drainNode: expected error not to have occured, %v", err)
	}
}

func Test_DrainNodeNegative(t *testing.T) {
	t.Log("Test_DrainNodeNegative: If drain process is unsuccessful, process should error")
	err := drainNode(stubKubectlPathFail, "some-node", 10, 0)
	if err == nil {
		t.Fatalf("drainNode: expected error to have occured, %v", err)
	}
}

func Test_RunCommandWithContextTimeout(t *testing.T) {
	t.Log("Test_RunCommandWithContextTimeout: should run a command with context successfully")
	err := runCommandWithContext("/bin/sleep", []string{"10"}, 1, 0)
	if err == nil {
		t.Fatalf("drainNode: expected error to have occured, %v", err)
	}
	expectedErr := "All attempts fail:\n#1: command execution timed out"
	if err.Error() != expectedErr {
		t.Fatalf("drainNode: expected error message to be: %v, got: %v", expectedErr, err)
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
