package service

import (
	"testing"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/autoscaling"
	"github.com/aws/aws-sdk-go/service/autoscaling/autoscalingiface"
)

type stubAutoscaling struct {
	autoscalingiface.AutoScalingAPI
	lifecycleHooks                            []*autoscaling.LifecycleHook
	timesCalledDescribeLifecycleHooks         int
	timesCalledRecordLifecycleActionHeartbeat int
	timesCalledCompleteLifecycleAction        int
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
	return &autoscaling.CompleteLifecycleActionOutput{}, nil
}

func (e *LifecycleEvent) _setDrainCompletedAfter(value bool, seconds int64) {
	time.Sleep(time.Duration(seconds)*time.Second + time.Duration(500)*time.Millisecond)
	e.drainCompleted = value
}

func Test_SendHeartbeatPositive(t *testing.T) {
	t.Log("Test_SendHeartbeatPositive: If drain is not complete, a heartbeat should be sent")
	stubber := &stubAutoscaling{}
	event := &LifecycleEvent{
		AutoScalingGroupName: "my-asg",
		EC2InstanceID:        "i-1234567890",
		LifecycleActionToken: "some-token-1234",
		LifecycleHookName:    "my-hook",
		drainCompleted:       false,
	}

	go event._setDrainCompletedAfter(true, 2)
	sendHeartbeat(stubber, event, 1)
	expectedHeartbeatCalls := 2

	if stubber.timesCalledRecordLifecycleActionHeartbeat != expectedHeartbeatCalls {
		t.Fatalf("expected timesCalledRecordLifecycleActionHeartbeat: %v, got: %v", expectedHeartbeatCalls, stubber.timesCalledRecordLifecycleActionHeartbeat)
	}
}

func Test_SendHeartbeatNegative(t *testing.T) {
	t.Log("Test_SendHeartbeatNegative: If drain is completed, heartbeat should not be sent")
	stubber := &stubAutoscaling{}
	event := &LifecycleEvent{
		AutoScalingGroupName: "my-asg",
		EC2InstanceID:        "i-1234567890",
		LifecycleActionToken: "some-token-1234",
		LifecycleHookName:    "my-hook",
		drainCompleted:       true,
	}

	sendHeartbeat(stubber, event, 1)
	expectedHeartbeatCalls := 0

	if stubber.timesCalledRecordLifecycleActionHeartbeat != expectedHeartbeatCalls {
		t.Fatalf("expected timesCalledRecordLifecycleActionHeartbeat: %v, got: %v", expectedHeartbeatCalls, stubber.timesCalledRecordLifecycleActionHeartbeat)
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
