package service

import (
	"context"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/autoscaling"
	"github.com/aws/aws-sdk-go/service/autoscaling/autoscalingiface"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

type stubAutoscaling struct {
	autoscalingiface.AutoScalingAPI
	lifecycleHooks                            []*autoscaling.LifecycleHook
	timesCalledDescribeLifecycleHooks         int
	timesCalledRecordLifecycleActionHeartbeat int
	timesCalledCompleteLifecycleAction        int
	lastCompleteAction                        string
}

func (a *stubAutoscaling) DescribeLifecycleHooks(input *autoscaling.DescribeLifecycleHooksInput) (*autoscaling.DescribeLifecycleHooksOutput, error) {
	a.timesCalledDescribeLifecycleHooks++
	return &autoscaling.DescribeLifecycleHooksOutput{LifecycleHooks: a.lifecycleHooks}, nil
}

func (a *stubAutoscaling) RecordLifecycleActionHeartbeat(input *autoscaling.RecordLifecycleActionHeartbeatInput) (*autoscaling.RecordLifecycleActionHeartbeatOutput, error) {
	a.timesCalledRecordLifecycleActionHeartbeat++
	return &autoscaling.RecordLifecycleActionHeartbeatOutput{}, nil
}

func (a *stubAutoscaling) CompleteLifecycleAction(input *autoscaling.CompleteLifecycleActionInput) (*autoscaling.CompleteLifecycleActionOutput, error) {
	a.timesCalledCompleteLifecycleAction++
	if input.LifecycleActionResult != nil {
		a.lastCompleteAction = *input.LifecycleActionResult
	}
	return &autoscaling.CompleteLifecycleActionOutput{}, nil
}

func (e *LifecycleEvent) _setEventCompletedAfter(value bool, seconds int64) {
	time.Sleep(time.Duration(seconds)*time.Second + time.Duration(500)*time.Millisecond)
	e.eventCompleted = value
}

func Test_SendHeartbeatPositive(t *testing.T) {
	t.Log("Test_SendHeartbeatPositive: If drain is not complete, a heartbeat should be sent")
	stubber := &stubAutoscaling{}
	kubeClient := fake.NewSimpleClientset()
	event := &LifecycleEvent{
		AutoScalingGroupName: "my-asg",
		EC2InstanceID:        "i-1234567890",
		LifecycleActionToken: "some-token-1234",
		LifecycleHookName:    "my-hook",
		drainCompleted:       false,
		heartbeatInterval:    3,
	}

	go event._setEventCompletedAfter(true, 2)
	sendHeartbeat(heartbeatConfig{
		asgClient:                 stubber,
		kubeClient:                kubeClient,
		event:                     event,
		maxTimeToProcessSeconds:   3600,
		maxTerminationGracePeriod: 900,
	})
	expectedHeartbeatCalls := 3

	if stubber.timesCalledRecordLifecycleActionHeartbeat != expectedHeartbeatCalls {
		t.Fatalf("expected timesCalledRecordLifecycleActionHeartbeat: %v, got: %v", expectedHeartbeatCalls, stubber.timesCalledRecordLifecycleActionHeartbeat)
	}
}

func Test_SendHeartbeatNegative(t *testing.T) {
	t.Log("Test_SendHeartbeatNegative: If event is completed, heartbeat should not be sent")
	stubber := &stubAutoscaling{}
	kubeClient := fake.NewSimpleClientset()
	event := &LifecycleEvent{
		AutoScalingGroupName: "my-asg",
		EC2InstanceID:        "i-1234567890",
		LifecycleActionToken: "some-token-1234",
		LifecycleHookName:    "my-hook",
		eventCompleted:       true,
		heartbeatInterval:    3,
	}

	sendHeartbeat(heartbeatConfig{
		asgClient:                 stubber,
		kubeClient:                kubeClient,
		event:                     event,
		maxTimeToProcessSeconds:   3600,
		maxTerminationGracePeriod: 900,
	})
	expectedHeartbeatCalls := 0

	if stubber.timesCalledRecordLifecycleActionHeartbeat != expectedHeartbeatCalls {
		t.Fatalf("expected timesCalledRecordLifecycleActionHeartbeat: %v, got: %v", expectedHeartbeatCalls, stubber.timesCalledRecordLifecycleActionHeartbeat)
	}
}

func Test_SendHeartbeatTimeoutDeletesPods(t *testing.T) {
	t.Log("Test_SendHeartbeatTimeoutDeletesPods: When max-time-to-process is reached, pods should be deleted and ABANDON called")
	stubber := &stubAutoscaling{}
	kubeClient := fake.NewSimpleClientset()

	node := &v1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "test-node"},
		Spec:       v1.NodeSpec{ProviderID: "aws:///us-west-2a/i-1234567890"},
	}
	kubeClient.CoreV1().Nodes().Create(context.Background(), node, metav1.CreateOptions{})

	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "default",
		},
		Spec: v1.PodSpec{NodeName: "test-node"},
	}
	kubeClient.CoreV1().Pods("default").Create(context.Background(), pod, metav1.CreateOptions{})

	event := &LifecycleEvent{
		AutoScalingGroupName: "my-asg",
		EC2InstanceID:        "i-1234567890",
		LifecycleActionToken: "some-token-1234",
		LifecycleHookName:    "my-hook",
		heartbeatInterval:    2,
		referencedNode:       *node,
	}

	sendHeartbeat(heartbeatConfig{
		asgClient:                 stubber,
		kubeClient:                kubeClient,
		event:                     event,
		maxTimeToProcessSeconds:   1,
		maxTerminationGracePeriod: 1,
	})

	if !event.eventCompleted {
		t.Fatal("expected eventCompleted to be true after timeout")
	}

	if stubber.timesCalledCompleteLifecycleAction != 1 {
		t.Fatalf("expected CompleteLifecycleAction called once, got: %v", stubber.timesCalledCompleteLifecycleAction)
	}

	if stubber.lastCompleteAction != AbandonAction {
		t.Fatalf("expected ABANDON action, got: %v", stubber.lastCompleteAction)
	}

	pods, _ := kubeClient.CoreV1().Pods("default").List(context.Background(), metav1.ListOptions{})
	if len(pods.Items) != 0 {
		t.Fatalf("expected pod to be deleted, but %d pods remain", len(pods.Items))
	}
}

func Test_SendHeartbeatTimeoutSkipsDaemonSetPods(t *testing.T) {
	t.Log("Test_SendHeartbeatTimeoutSkipsDaemonSetPods: DaemonSet pods should not be deleted on timeout")
	stubber := &stubAutoscaling{}
	kubeClient := fake.NewSimpleClientset()

	node := &v1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "test-node"},
		Spec:       v1.NodeSpec{ProviderID: "aws:///us-west-2a/i-1234567890"},
	}
	kubeClient.CoreV1().Nodes().Create(context.Background(), node, metav1.CreateOptions{})

	dsPod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ds-pod",
			Namespace: "kube-system",
			OwnerReferences: []metav1.OwnerReference{
				{Kind: "DaemonSet", Name: "my-ds", APIVersion: "apps/v1"},
			},
		},
		Spec: v1.PodSpec{NodeName: "test-node"},
	}
	kubeClient.CoreV1().Pods("kube-system").Create(context.Background(), dsPod, metav1.CreateOptions{})

	regularPod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "regular-pod",
			Namespace: "default",
		},
		Spec: v1.PodSpec{NodeName: "test-node"},
	}
	kubeClient.CoreV1().Pods("default").Create(context.Background(), regularPod, metav1.CreateOptions{})

	event := &LifecycleEvent{
		AutoScalingGroupName: "my-asg",
		EC2InstanceID:        "i-1234567890",
		LifecycleActionToken: "some-token-1234",
		LifecycleHookName:    "my-hook",
		heartbeatInterval:    2,
		referencedNode:       *node,
	}

	sendHeartbeat(heartbeatConfig{
		asgClient:                 stubber,
		kubeClient:                kubeClient,
		event:                     event,
		maxTimeToProcessSeconds:   1,
		maxTerminationGracePeriod: 1,
	})

	dsPods, _ := kubeClient.CoreV1().Pods("kube-system").List(context.Background(), metav1.ListOptions{})
	if len(dsPods.Items) != 1 {
		t.Fatalf("expected daemonset pod to survive, but got %d pods", len(dsPods.Items))
	}

	regularPods, _ := kubeClient.CoreV1().Pods("default").List(context.Background(), metav1.ListOptions{})
	if len(regularPods.Items) != 0 {
		t.Fatalf("expected regular pod to be deleted, but got %d pods", len(regularPods.Items))
	}
}

func Test_CompleteLifecycleAction(t *testing.T) {
	t.Log("Test_CompleteLifecycleAction: should be able to complete a lifecycle action")
	stubber := &stubAutoscaling{}
	event := &LifecycleEvent{
		AutoScalingGroupName: "my-asg",
		EC2InstanceID:        "i-1234567890",
		LifecycleActionToken: "some-token-1234",
		LifecycleHookName:    "my-hook",
		drainCompleted:       true,
	}

	completeLifecycleAction(stubber, *event, ContinueAction)
	completeLifecycleAction(stubber, *event, AbandonAction)
	expectedCalls := 2

	if stubber.timesCalledCompleteLifecycleAction != expectedCalls {
		t.Fatalf("expected timesCalledRecordLifecycleActionHeartbeat: %v, got: %v", expectedCalls, stubber.timesCalledCompleteLifecycleAction)
	}
}

func Test_GetHookHeartbeatIntervalPositive(t *testing.T) {
	t.Log("Test_GetHookHeartbeatIntervalPositive: should be able get a lifecycle hook's heartbeat timeout interval if it exists")
	stubber := &stubAutoscaling{
		lifecycleHooks: []*autoscaling.LifecycleHook{
			{
				AutoScalingGroupName: aws.String("my-asg"),
				HeartbeatTimeout:     aws.Int64(60),
			},
		},
	}
	event := &LifecycleEvent{
		AutoScalingGroupName: "my-asg",
		EC2InstanceID:        "i-1234567890",
		LifecycleActionToken: "some-token-1234",
		LifecycleHookName:    "my-hook",
		drainCompleted:       true,
	}

	interval, err := getHookHeartbeatInterval(stubber, event.AutoScalingGroupName, event.LifecycleHookName)
	if err != nil {
		t.Fatalf("getHookHeartbeatInterval: expected error not to have occured, %v", err)
	}

	expectedCalls := 1
	expectedInterval := int64(60)

	if stubber.timesCalledDescribeLifecycleHooks != expectedCalls {
		t.Fatalf("expected timesCalledDescribeLifecycleHooks: %v, got: %v", expectedCalls, stubber.timesCalledDescribeLifecycleHooks)
	}

	if interval != expectedInterval {
		t.Fatalf("expected interval: %v, got: %v", expectedInterval, interval)
	}
}

func Test_GetHookHeartbeatIntervalNegative(t *testing.T) {
	t.Log("Test_GetHookHeartbeatIntervalNegative: should not be able get a lifecycle hook's heartbeat timeout interval if it does not exists")
	stubber := &stubAutoscaling{
		lifecycleHooks: []*autoscaling.LifecycleHook{},
	}
	event := &LifecycleEvent{
		AutoScalingGroupName: "my-asg",
		EC2InstanceID:        "i-1234567890",
		LifecycleActionToken: "some-token-1234",
		LifecycleHookName:    "my-hook",
		drainCompleted:       true,
	}

	interval, err := getHookHeartbeatInterval(stubber, event.AutoScalingGroupName, event.LifecycleHookName)
	if err == nil {
		t.Fatal("getHookHeartbeatInterval: expected error to have occured")
	}

	expectedCalls := 1
	expectedInterval := int64(0)

	if stubber.timesCalledDescribeLifecycleHooks != expectedCalls {
		t.Fatalf("expected timesCalledDescribeLifecycleHooks: %v, got: %v", expectedCalls, stubber.timesCalledDescribeLifecycleHooks)
	}

	if interval != expectedInterval {
		t.Fatalf("expected interval: %v, got: %v", expectedInterval, interval)
	}
}
