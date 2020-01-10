package service

import (
	"fmt"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/autoscaling"
	"github.com/aws/aws-sdk-go/service/elb"
	"github.com/aws/aws-sdk-go/service/elbv2"
	"github.com/aws/aws-sdk-go/service/sqs"
	"github.com/pkg/errors"

	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func init() {
	ThreadJitterRangeSeconds = 1
}

func Test_RejectHandler(t *testing.T) {
	t.Log("Test_RejectHandler: should handle rejections")
	var (
		sqsStubber = &stubSQS{}
	)

	asgStubber := &stubAutoscaling{
		lifecycleHooks: []*autoscaling.LifecycleHook{
			{
				AutoScalingGroupName: aws.String("my-asg"),
				HeartbeatTimeout:     aws.Int64(60),
			},
		},
	}

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

	fakeMessage := &sqs.Message{
		// invalid instance id
		Body:          aws.String(`{"LifecycleHookName":"my-hook","AccountId":"12345689012","RequestId":"63f5b5c2-58b3-0574-b7d5-b3162d0268f0","LifecycleTransition":"autoscaling:EC2_INSTANCE_TERMINATING","AutoScalingGroupName":"my-asg","Service":"AWS Auto Scaling","Time":"2019-09-27T02:39:14.183Z","EC2InstanceId":"","LifecycleActionToken":"cc34960c-1e41-4703-a665-bdb3e5b81ad3"}`),
		ReceiptHandle: aws.String("MbZj6wDWli+JvwwJaBV+3dcjk2YW2vA3+STFFljTM8tJJg6HRG6PYSasuWXPJB+Cw="),
	}

	mgr := New(auth, ctx)
	mgr.newWorker(fakeMessage)

	expectedFailedEvents := 1
	if mgr.rejectedEvents != expectedFailedEvents {
		t.Fatalf("expected rejected events: %v, got: %v", expectedFailedEvents, mgr.rejectedEvents)
	}

	expectedDeleteMessageEvents := 1
	if sqsStubber.timesCalledDeleteMessage != expectedDeleteMessageEvents {
		t.Fatalf("expected deleted events: %v, got: %v", expectedDeleteMessageEvents, sqsStubber.timesCalledDeleteMessage)
	}
}

func Test_FailHandler(t *testing.T) {
	t.Log("Test_FailHandler: should handle failures")
	var (
		sqsStubber = &stubSQS{}
	)

	asgStubber := &stubAutoscaling{
		lifecycleHooks: []*autoscaling.LifecycleHook{
			{
				AutoScalingGroupName: aws.String("my-asg"),
				HeartbeatTimeout:     aws.Int64(60),
			},
		},
	}

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
		startTime:            time.Now().Add(time.Duration(-1) * time.Second),
	}

	mgr := New(auth, ctx)
	err := errors.New("some error occured")
	mgr.FailEvent(err, event, true)

	expectedFailedEvents := 1
	if mgr.failedEvents != expectedFailedEvents {
		t.Fatalf("expected failed events: %v, got: %v", expectedFailedEvents, mgr.failedEvents)
	}

	expectedDeleteMessageEvents := 1
	if sqsStubber.timesCalledDeleteMessage != expectedDeleteMessageEvents {
		t.Fatalf("expected deleted events: %v, got: %v", expectedDeleteMessageEvents, sqsStubber.timesCalledDeleteMessage)
	}

	expectedEventCompleted := true
	if event.eventCompleted != expectedEventCompleted {
		t.Fatalf("expected event completed: %v, got: %v", expectedEventCompleted, event.eventCompleted)
	}
}

func Test_Process(t *testing.T) {
	t.Log("Test_Process: should process events")
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
		heartbeatInterval:    3,
	}

	g := New(auth, ctx)
	err := g.handleEvent(event)
	if err != nil {
		t.Fatalf("handleEvent: expected error not to have occured, %v", err)
	}

	if event.drainCompleted != true {
		t.Fatal("handleEvent: expected drainCompleted to be true, got: false")
	}
}

func Test_HandleEventWithDeregister(t *testing.T) {
	t.Log("Test_HandleEvent: should successfully handle events")
	var (
		asgStubber       = &stubAutoscaling{}
		sqsStubber       = &stubSQS{}
		arn              = "arn:aws:elasticloadbalancing:us-west-2:0000000000:targetgroup/targetgroup-name/some-id"
		elbName          = "my-classic-elb"
		instanceID       = "i-123486890234"
		port       int64 = 122233
	)

	elbv2Stubber := &stubELBv2{
		targetHealthDescriptions: []*elbv2.TargetHealthDescription{
			{
				Target: &elbv2.TargetDescription{
					Id:   aws.String(instanceID),
					Port: aws.Int64(port),
				},
			},
		},
		targetGroups: []*elbv2.TargetGroup{
			{
				TargetGroupArn: aws.String(arn),
			},
		},
	}

	elbStubber := &stubELB{
		loadBalancerDescriptions: []*elb.LoadBalancerDescription{
			{
				LoadBalancerName: aws.String(elbName),
			},
		},
		instanceStates: []*elb.InstanceState{
			{
				InstanceId: aws.String(instanceID),
			},
		},
	}

	auth := Authenticator{
		ScalingGroupClient: asgStubber,
		SQSClient:          sqsStubber,
		ELBv2Client:        elbv2Stubber,
		ELBClient:          elbStubber,
		KubernetesClient:   fake.NewSimpleClientset(),
	}

	ctx := ManagerContext{
		KubectlLocalPath:       stubKubectlPathSuccess,
		QueueName:              "my-queue",
		Region:                 "us-west-2",
		DrainTimeoutSeconds:    1,
		PollingIntervalSeconds: 1,
		WithDeregister:         true,
	}

	fakeNodes := []v1.Node{
		{
			Spec: v1.NodeSpec{
				ProviderID: fmt.Sprintf("aws:///us-west-2a/%v", instanceID),
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
		EC2InstanceID:        instanceID,
		LifecycleActionToken: "cc34960c-1e41-4703-a665-bdb3e5b81ad3",
		receiptHandle:        "MbZj6wDWli+JvwwJaBV+3dcjk2YW2vA3+STFFljTM8tJJg6HRG6PYSasuWXPJB+Cw=",
		heartbeatInterval:    3,
	}

	g := New(auth, ctx)
	err := g.handleEvent(event)
	if err != nil {
		t.Fatalf("handleEvent: expected error not to have occured, %v", err)
	}

	if event.drainCompleted != true {
		t.Fatal("handleEvent: expected drainCompleted to be true, got: false")
	}

	if event.deregisterCompleted != true {
		t.Fatal("handleEvent: expected deregisterCompleted to be true, got: false")
	}
}

func Test_HandleEventWithDeregisterError(t *testing.T) {
	t.Log("Test_HandleEvent: should successfully handle events")
	var (
		asgStubber       = &stubAutoscaling{}
		sqsStubber       = &stubSQS{}
		arn              = "arn:aws:elasticloadbalancing:us-west-2:0000000000:targetgroup/targetgroup-name/some-id"
		elbName          = "my-classic-elb"
		instanceID       = "i-123486890234"
		port       int64 = 122233
	)

	elbv2Stubber := &stubErrorELBv2{
		targetHealthDescriptions: []*elbv2.TargetHealthDescription{
			{
				Target: &elbv2.TargetDescription{
					Id:   aws.String(instanceID),
					Port: aws.Int64(port),
				},
			},
		},
		targetGroups: []*elbv2.TargetGroup{
			{
				TargetGroupArn: aws.String(arn),
			},
		},
		failHint: "DeregisterTargets",
	}

	elbStubber := &stubErrorELB{
		loadBalancerDescriptions: []*elb.LoadBalancerDescription{
			{
				LoadBalancerName: aws.String(elbName),
			},
		},
		instanceStates: []*elb.InstanceState{
			{
				InstanceId: aws.String(instanceID),
			},
		},
		failHint: "DeregisterInstancesFromLoadBalancer",
	}

	auth := Authenticator{
		ScalingGroupClient: asgStubber,
		SQSClient:          sqsStubber,
		ELBv2Client:        elbv2Stubber,
		ELBClient:          elbStubber,
		KubernetesClient:   fake.NewSimpleClientset(),
	}

	ctx := ManagerContext{
		KubectlLocalPath:       stubKubectlPathSuccess,
		QueueName:              "my-queue",
		Region:                 "us-west-2",
		DrainTimeoutSeconds:    1,
		PollingIntervalSeconds: 1,
		WithDeregister:         true,
	}

	fakeNodes := []v1.Node{
		{
			Spec: v1.NodeSpec{
				ProviderID: fmt.Sprintf("aws:///us-west-2a/%v", instanceID),
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
		EC2InstanceID:        instanceID,
		LifecycleActionToken: "cc34960c-1e41-4703-a665-bdb3e5b81ad3",
		receiptHandle:        "MbZj6wDWli+JvwwJaBV+3dcjk2YW2vA3+STFFljTM8tJJg6HRG6PYSasuWXPJB+Cw=",
		heartbeatInterval:    3,
	}

	g := New(auth, ctx)
	err := g.handleEvent(event)
	if err == nil {
		t.Fatalf("handleEvent: expected error but did not get an error")
	}
}

func Test_HandleEventWaitUntilTargetDeregisterError(t *testing.T) {
	t.Log("Test_HandleEvent: should successfully handle events")
	var (
		asgStubber       = &stubAutoscaling{}
		sqsStubber       = &stubSQS{}
		arn              = "arn:aws:elasticloadbalancing:us-west-2:0000000000:targetgroup/targetgroup-name/some-id"
		elbName          = "my-classic-elb"
		instanceID       = "i-123486890234"
		port       int64 = 122233
	)

	elbv2Stubber := &stubErrorELBv2{
		targetHealthDescriptions: []*elbv2.TargetHealthDescription{
			{
				Target: &elbv2.TargetDescription{
					Id:   aws.String(instanceID),
					Port: aws.Int64(port),
				},
			},
		},
		targetGroups: []*elbv2.TargetGroup{
			{
				TargetGroupArn: aws.String(arn),
			},
		},
		failHint: "WaitUntilTargetDeregisteredWithContext",
	}

	elbStubber := &stubErrorELB{
		loadBalancerDescriptions: []*elb.LoadBalancerDescription{
			{
				LoadBalancerName: aws.String(elbName),
			},
		},
		instanceStates: []*elb.InstanceState{
			{
				InstanceId: aws.String(instanceID),
			},
		},
		failHint: "WaitUntilInstanceDeregisteredWithContext",
	}

	auth := Authenticator{
		ScalingGroupClient: asgStubber,
		SQSClient:          sqsStubber,
		ELBv2Client:        elbv2Stubber,
		ELBClient:          elbStubber,
		KubernetesClient:   fake.NewSimpleClientset(),
	}

	ctx := ManagerContext{
		KubectlLocalPath:       stubKubectlPathSuccess,
		QueueName:              "my-queue",
		Region:                 "us-west-2",
		DrainTimeoutSeconds:    1,
		PollingIntervalSeconds: 1,
		WithDeregister:         true,
	}

	fakeNodes := []v1.Node{
		{
			Spec: v1.NodeSpec{
				ProviderID: fmt.Sprintf("aws:///us-west-2a/%v", instanceID),
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
		EC2InstanceID:        instanceID,
		LifecycleActionToken: "cc34960c-1e41-4703-a665-bdb3e5b81ad3",
		receiptHandle:        "MbZj6wDWli+JvwwJaBV+3dcjk2YW2vA3+STFFljTM8tJJg6HRG6PYSasuWXPJB+Cw=",
		heartbeatInterval:    3,
	}

	g := New(auth, ctx)
	err := g.handleEvent(event)
	if err == nil {
		t.Fatalf("handleEvent: expected error but did not get an error")
	}
}

func Test_Poller(t *testing.T) {
	t.Log("Test_Poller: should deliver messages from sqs to channel")
	var (
		fakeQueueName   = "my-queue"
		fakeMessageBody = "message-body"
		fakeEventStream = make(chan *sqs.Message, 0)
	)
	sqsStubber := &stubSQS{
		FakeQueueName: fakeQueueName,
		FakeQueueMessages: []*sqs.Message{
			{
				Body: aws.String(fakeMessageBody),
			},
		},
	}

	auth := Authenticator{
		SQSClient: sqsStubber,
	}

	ctx := ManagerContext{
		QueueName:              "my-queue",
		Region:                 "us-west-2",
		PollingIntervalSeconds: 10,
	}

	mgr := New(auth, ctx)
	mgr.eventStream = fakeEventStream

	go mgr.newPoller()
	time.Sleep(time.Duration(1) * time.Second)

	if sqsStubber.timesCalledReceiveMessage == 0 {
		t.Fatalf("expected timesCalledReceiveMessage: N>0, got: 0")
	}

	message := <-fakeEventStream
	if aws.StringValue(message.Body) != fakeMessageBody {
		t.Fatalf("expected message body: %v, got: %v", fakeMessageBody, message.Body)
	}
}

func Test_Worker(t *testing.T) {
	t.Log("Test_Worker: should start processing messages")
	var (
		sqsStubber = &stubSQS{}
	)

	asgStubber := &stubAutoscaling{
		lifecycleHooks: []*autoscaling.LifecycleHook{
			{
				AutoScalingGroupName: aws.String("my-asg"),
				HeartbeatTimeout:     aws.Int64(60),
			},
		},
	}

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

	fakeMessage := &sqs.Message{
		Body:          aws.String(`{"LifecycleHookName":"my-hook","AccountId":"12345689012","RequestId":"63f5b5c2-58b3-0574-b7d5-b3162d0268f0","LifecycleTransition":"autoscaling:EC2_INSTANCE_TERMINATING","AutoScalingGroupName":"my-asg","Service":"AWS Auto Scaling","Time":"2019-09-27T02:39:14.183Z","EC2InstanceId":"i-123486890234","LifecycleActionToken":"cc34960c-1e41-4703-a665-bdb3e5b81ad3"}`),
		ReceiptHandle: aws.String("MbZj6wDWli+JvwwJaBV+3dcjk2YW2vA3+STFFljTM8tJJg6HRG6PYSasuWXPJB+Cw="),
	}

	mgr := New(auth, ctx)
	mgr.newWorker(fakeMessage)
	expectedCompletedEvents := 1

	if mgr.completedEvents != expectedCompletedEvents {
		t.Fatalf("expected completed events: %v, got: %v", expectedCompletedEvents, mgr.completedEvents)
	}

}
