package service

import (
	"testing"

	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func Test_Process(t *testing.T) {
	t.Log("Test_Process: ")
	asgStubber := &stubAutoscaling{}
	sqsStubber := &stubSQS{}
	auth := Authenticator{
		ScalingGroupClient: asgStubber,
		SQSClient:          sqsStubber,
		KubernetesClient:   fake.NewSimpleClientset(),
	}
	ctx := ManagerContext{
		KubectlLocalPath:       stubKubectlPathSuccess,
		QueueName:              "my-queue",
		Region:                 "us-west-2",
		DrainTimeoutSeconds:    1,
		PollingIntervalSeconds: 1,
	}

	fakeNodes := []v1.Node{
		{
			Spec: v1.NodeSpec{
				ProviderID: "aws:///us-west-2a/i-123486890234",
			},
		},
		{
			Spec: v1.NodeSpec{
				ProviderID: "aws:///us-west-2c/i-22222222222222222",
			},
		},
	}

	for _, node := range fakeNodes {
		auth.KubernetesClient.CoreV1().Nodes().Create(&node)
	}

	event := &LifecycleEvent{
		LifecycleHookName:    "my-hook",
		AccountID:            "12345689012",
		RequestID:            "63f5b5c2-58b3-0574-b7d5-b3162d0268f0",
		LifecycleTransition:  "autoscaling:EC2_INSTANCE_TERMINATING",
		AutoScalingGroupName: "my-asg",
		EC2InstanceID:        "i-123486890234",
		LifecycleActionToken: "cc34960c-1e41-4703-a665-bdb3e5b81ad3",
		receiptHandle:        "MbZj6wDWli+JvwwJaBV+3dcjk2YW2vA3+STFFljTM8tJJg6HRG6PYSasuWXPJB+Cw=",
		heartbeatInterval:    2,
	}

	if !event.IsValid() {
		t.Fatal("Process: expected IsValid to be true, got: false")
	}

	g := New(auth, ctx)
	g.Process(event)

	if event.drainCompleted != true {
		t.Fatal("handleEvent: expected drainCompleted to be true, got: false")
	}

	if asgStubber.timesCalledCompleteLifecycleAction != 1 {
		t.Fatalf("Process: expected timesCalledCompleteLifecycleAction to be 1, got: %v", asgStubber.timesCalledCompleteLifecycleAction)
	}

	if sqsStubber.timesCalledDeleteMessage != 1 {
		t.Fatalf("Process: expected timesCalledDeleteMessage to be 1, got: %v", sqsStubber.timesCalledDeleteMessage)
	}
}

func Test_HandleEvent(t *testing.T) {
	t.Log("Test_HandleEvent: should successfully handle events")
	asgStubber := &stubAutoscaling{}
	sqsStubber := &stubSQS{}
	auth := Authenticator{
		ScalingGroupClient: asgStubber,
		SQSClient:          sqsStubber,
		KubernetesClient:   fake.NewSimpleClientset(),
	}
	ctx := ManagerContext{
		KubectlLocalPath:       stubKubectlPathSuccess,
		QueueName:              "my-queue",
		Region:                 "us-west-2",
		DrainTimeoutSeconds:    1,
		PollingIntervalSeconds: 1,
	}

	fakeNodes := []v1.Node{
		{
			Spec: v1.NodeSpec{
				ProviderID: "aws:///us-west-2a/i-123486890234",
			},
		},
		{
			Spec: v1.NodeSpec{
				ProviderID: "aws:///us-west-2c/i-22222222222222222",
			},
		},
	}

	for _, node := range fakeNodes {
		auth.KubernetesClient.CoreV1().Nodes().Create(&node)
	}

	event := &LifecycleEvent{
		LifecycleHookName:    "my-hook",
		AccountID:            "12345689012",
		RequestID:            "63f5b5c2-58b3-0574-b7d5-b3162d0268f0",
		LifecycleTransition:  "autoscaling:EC2_INSTANCE_TERMINATING",
		AutoScalingGroupName: "my-asg",
		EC2InstanceID:        "i-123486890234",
		LifecycleActionToken: "cc34960c-1e41-4703-a665-bdb3e5b81ad3",
		receiptHandle:        "MbZj6wDWli+JvwwJaBV+3dcjk2YW2vA3+STFFljTM8tJJg6HRG6PYSasuWXPJB+Cw=",
	}

	g := New(auth, ctx)
	err := g.handleEvent(event)
	if err != nil {
		t.Fatalf("handleEvent: expected error not to have occured, %v", err)
	}

	if event.drainCompleted != true {
		t.Fatal("handleEvent: expected drainCompleted to be true, got: false")
	}

	if asgStubber.timesCalledCompleteLifecycleAction != 1 {
		t.Fatalf("handleEvent: expected timesCalledCompleteLifecycleAction to be 1, got: %v", asgStubber.timesCalledCompleteLifecycleAction)
	}
}
