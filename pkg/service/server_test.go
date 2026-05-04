package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	v1 "k8s.io/api/core/v1"
	apimachinery_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	k8stesting "k8s.io/client-go/testing"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/autoscaling"
	"github.com/aws/aws-sdk-go/service/elb"
	"github.com/aws/aws-sdk-go/service/elbv2"
	"github.com/aws/aws-sdk-go/service/sqs"
	"github.com/pkg/errors"
	"golang.org/x/sync/semaphore"
	"k8s.io/client-go/kubernetes/fake"
)

func init() {
	ThreadJitterRangeSeconds = 0
	IterationJitterRangeSeconds = 0
	WaiterMinDelay = 1 * time.Second
	WaiterMaxDelay = 2 * time.Second
	WaiterMaxAttempts = 3
	NodeAgeCacheTTL = 100
	waitForPodsToBeDeletedPollInterval = 10 * time.Millisecond
	EscalateDrainFailureWaitSlackSeconds = 30
}

func _completeEventAfter(event *LifecycleEvent, t time.Duration) {
	time.Sleep(t)
	event.SetEventCompleted(true)
}

func _newBasicContext() ManagerContext {
	return ManagerContext{
		KubectlLocalPath:          stubKubectlPathSuccess,
		QueueName:                 "my-queue",
		Region:                    "us-west-2",
		DrainTimeoutSeconds:       1,
		DrainRetryAttempts:        3,
		PollingIntervalSeconds:    1,
		MaxDrainConcurrency:       semaphore.NewWeighted(32),
		MaxTimeToProcessSeconds:   3600,
		MaxTerminationGracePeriod: 900,
	}
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
	ctx := _newBasicContext()

	fakeMessage := &sqs.Message{
		// invalid instance id
		Body:          aws.String(`{"LifecycleHookName":"my-hook","AccountId":"12345689012","RequestId":"63f5b5c2-58b3-0574-b7d5-b3162d0268f0","LifecycleTransition":"autoscaling:EC2_INSTANCE_TERMINATING","AutoScalingGroupName":"my-asg","Service":"AWS Auto Scaling","Time":"2019-09-27T02:39:14.183Z","EC2InstanceId":"","LifecycleActionToken":"cc34960c-1e41-4703-a665-bdb3e5b81ad3"}`),
		ReceiptHandle: aws.String("MbZj6wDWli+JvwwJaBV+3dcjk2YW2vA3+STFFljTM8tJJg6HRG6PYSasuWXPJB+Cw="),
	}

	mgr := New(auth, ctx)
	_, err := mgr.newEvent(fakeMessage, "some-queue")
	if err == nil {
		t.Fatalf("expected rejected events: %v, got: %v", 1, mgr.rejectedEvents)
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
	ctx := _newBasicContext()

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
	ctx := _newBasicContext()

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
		auth.KubernetesClient.CoreV1().Nodes().Create(context.Background(), &node, apimachinery_v1.CreateOptions{})
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
	ctx := _newBasicContext()

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
		auth.KubernetesClient.CoreV1().Nodes().Create(context.Background(), &node, apimachinery_v1.CreateOptions{})
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
				TargetHealth: &elbv2.TargetHealth{
					State: aws.String(elbv2.TargetHealthStateEnumUnused),
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
				State:      aws.String("OutOfService"),
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

	ctx := _newBasicContext()
	ctx.WithDeregister = true
	ctx.DeregisterTargetTypes = []string{TargetTypeClassicELB.String(), TargetTypeTargetGroup.String()}

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
		auth.KubernetesClient.CoreV1().Nodes().Create(context.Background(), &node, apimachinery_v1.CreateOptions{})
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
				TargetHealth: &elbv2.TargetHealth{
					State: aws.String(elbv2.TargetHealthStateEnumUnused),
				},
			},
		},
		targetGroups: []*elbv2.TargetGroup{
			{
				TargetGroupArn: aws.String(arn),
			},
		},
		failHint: elb.ErrCodeAccessPointNotFoundException,
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
				State:      aws.String("OutOfService"),
			},
		},
		failHint: "some-other-error",
	}

	auth := Authenticator{
		ScalingGroupClient: asgStubber,
		SQSClient:          sqsStubber,
		ELBv2Client:        elbv2Stubber,
		ELBClient:          elbStubber,
		KubernetesClient:   fake.NewSimpleClientset(),
	}

	ctx := _newBasicContext()
	ctx.WithDeregister = true
	ctx.DeregisterTargetTypes = []string{TargetTypeClassicELB.String(), TargetTypeTargetGroup.String()}

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
		auth.KubernetesClient.CoreV1().Nodes().Create(context.Background(), &node, apimachinery_v1.CreateOptions{})
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

	ctx := _newBasicContext()

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

	ctx := _newBasicContext()

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
		auth.KubernetesClient.CoreV1().Nodes().Create(context.Background(), &node, apimachinery_v1.CreateOptions{})
	}

	fakeMessage := &sqs.Message{
		Body:          aws.String(`{"LifecycleHookName":"my-hook","AccountId":"12345689012","RequestId":"63f5b5c2-58b3-0574-b7d5-b3162d0268f0","LifecycleTransition":"autoscaling:EC2_INSTANCE_TERMINATING","AutoScalingGroupName":"my-asg","Service":"AWS Auto Scaling","Time":"2019-09-27T02:39:14.183Z","EC2InstanceId":"i-123486890234","LifecycleActionToken":"cc34960c-1e41-4703-a665-bdb3e5b81ad3"}`),
		ReceiptHandle: aws.String("MbZj6wDWli+JvwwJaBV+3dcjk2YW2vA3+STFFljTM8tJJg6HRG6PYSasuWXPJB+Cw="),
	}

	mgr := New(auth, ctx)
	event, err := mgr.newEvent(fakeMessage, "some-queue")
	if err != nil {
		t.Fatalf("failed to create event: %v", err)
	}

	mgr.Process(event)
	expectedCompletedEvents := 1

	if mgr.completedEvents != expectedCompletedEvents {
		t.Fatalf("expected completed events: %v, got: %v", expectedCompletedEvents, mgr.completedEvents)
	}

}

func Test_EscalateDrainFailureSuccess(t *testing.T) {
	t.Log("Test_EscalateDrainFailureSuccess: drain failed -> escalation force-deletes pods, returns nil")
	kubeClient := fake.NewSimpleClientset()

	node := &v1.Node{
		ObjectMeta: apimachinery_v1.ObjectMeta{Name: "test-node"},
		Spec:       v1.NodeSpec{ProviderID: "aws:///us-west-2a/i-1234567890"},
	}
	kubeClient.CoreV1().Nodes().Create(context.Background(), node, apimachinery_v1.CreateOptions{})

	pod := &v1.Pod{
		ObjectMeta: apimachinery_v1.ObjectMeta{Name: "test-pod", Namespace: "default"},
		Spec:       v1.PodSpec{NodeName: "test-node"},
	}
	kubeClient.CoreV1().Pods("default").Create(context.Background(), pod, apimachinery_v1.CreateOptions{})

	event := &LifecycleEvent{
		AutoScalingGroupName: "my-asg",
		EC2InstanceID:        "i-1234567890",
		LifecycleHookName:    "my-hook",
		referencedNode:       *node,
	}

	if err := escalateDrainFailure(event, kubeClient, 1); err != nil {
		t.Fatalf("expected escalateDrainFailure to succeed, got: %v", err)
	}

	pods, _ := kubeClient.CoreV1().Pods("default").List(context.Background(), apimachinery_v1.ListOptions{})
	if len(pods.Items) != 0 {
		t.Fatalf("expected pod to be deleted by escalation, but %d pods remain", len(pods.Items))
	}
}

func Test_EscalateDrainFailureDaemonSetSurvives(t *testing.T) {
	t.Log("Test_EscalateDrainFailureDaemonSetSurvives: DaemonSet pods are not deleted during escalation")
	kubeClient := fake.NewSimpleClientset()

	node := &v1.Node{
		ObjectMeta: apimachinery_v1.ObjectMeta{Name: "test-node"},
		Spec:       v1.NodeSpec{ProviderID: "aws:///us-west-2a/i-1234567890"},
	}
	kubeClient.CoreV1().Nodes().Create(context.Background(), node, apimachinery_v1.CreateOptions{})

	dsPod := &v1.Pod{
		ObjectMeta: apimachinery_v1.ObjectMeta{
			Name:      "ds-pod",
			Namespace: "kube-system",
			OwnerReferences: []apimachinery_v1.OwnerReference{
				{Kind: "DaemonSet", Name: "my-ds", APIVersion: "apps/v1"},
			},
		},
		Spec: v1.PodSpec{NodeName: "test-node"},
	}
	kubeClient.CoreV1().Pods("kube-system").Create(context.Background(), dsPod, apimachinery_v1.CreateOptions{})

	regularPod := &v1.Pod{
		ObjectMeta: apimachinery_v1.ObjectMeta{Name: "regular-pod", Namespace: "default"},
		Spec:       v1.PodSpec{NodeName: "test-node"},
	}
	kubeClient.CoreV1().Pods("default").Create(context.Background(), regularPod, apimachinery_v1.CreateOptions{})

	event := &LifecycleEvent{
		AutoScalingGroupName: "my-asg",
		EC2InstanceID:        "i-1234567890",
		LifecycleHookName:    "my-hook",
		referencedNode:       *node,
	}

	if err := escalateDrainFailure(event, kubeClient, 1); err != nil {
		t.Fatalf("expected escalateDrainFailure to succeed, got: %v", err)
	}

	dsPods, _ := kubeClient.CoreV1().Pods("kube-system").List(context.Background(), apimachinery_v1.ListOptions{})
	if len(dsPods.Items) != 1 {
		t.Fatalf("expected daemonset pod to survive escalation, got %d pods", len(dsPods.Items))
	}
	regularPods, _ := kubeClient.CoreV1().Pods("default").List(context.Background(), apimachinery_v1.ListOptions{})
	if len(regularPods.Items) != 0 {
		t.Fatalf("expected regular pod to be deleted by escalation, got %d pods", len(regularPods.Items))
	}
}

func Test_EscalateDrainFailureGracePeriodZero(t *testing.T) {
	t.Log("Test_EscalateDrainFailureGracePeriodZero: when grace period <= 0, escalation refuses to run and returns an error")
	kubeClient := fake.NewSimpleClientset()
	node := &v1.Node{
		ObjectMeta: apimachinery_v1.ObjectMeta{Name: "test-node"},
		Spec:       v1.NodeSpec{ProviderID: "aws:///us-west-2a/i-1234567890"},
	}
	kubeClient.CoreV1().Nodes().Create(context.Background(), node, apimachinery_v1.CreateOptions{})

	event := &LifecycleEvent{
		AutoScalingGroupName: "my-asg",
		EC2InstanceID:        "i-1234567890",
		LifecycleHookName:    "my-hook",
		referencedNode:       *node,
	}

	if err := escalateDrainFailure(event, kubeClient, 0); err == nil {
		t.Fatal("expected escalateDrainFailure to return an error when grace period is 0, got nil")
	}
}

func Test_EscalateDrainFailureNegativeGrace(t *testing.T) {
	t.Log("Test_EscalateDrainFailureNegativeGrace: negative max-termination-grace-period is treated as disabled")
	kubeClient := fake.NewSimpleClientset()
	node := &v1.Node{
		ObjectMeta: apimachinery_v1.ObjectMeta{Name: "test-node"},
		Spec:       v1.NodeSpec{ProviderID: "aws:///us-west-2a/i-1234567890"},
	}
	kubeClient.CoreV1().Nodes().Create(context.Background(), node, apimachinery_v1.CreateOptions{})

	event := &LifecycleEvent{
		AutoScalingGroupName: "my-asg",
		EC2InstanceID:        "i-1234567890",
		LifecycleHookName:    "my-hook",
		referencedNode:       *node,
	}

	if err := escalateDrainFailure(event, kubeClient, -1); err == nil {
		t.Fatal("expected escalateDrainFailure to return an error when grace period is negative, got nil")
	}
}

func Test_EscalateDrainFailurePodsRemainAfterDelete(t *testing.T) {
	t.Log("Test_EscalateDrainFailurePodsRemainAfterDelete: simulates stubborn pod — DELETE succeeds but pod never leaves the API (wait times out)")
	defer func() { EscalateDrainFailureWaitSlackSeconds = 30 }()
	EscalateDrainFailureWaitSlackSeconds = 0

	kubeClient := fake.NewSimpleClientset()
	kubeClient.PrependReactor("delete", "pods", func(action k8stesting.Action) (handled bool, ret runtime.Object, err error) {
		// Succeed without removing the pod from the fake store (stubborn pod / broken API semantics).
		return true, nil, nil
	})

	node := &v1.Node{
		ObjectMeta: apimachinery_v1.ObjectMeta{Name: "test-node"},
		Spec:       v1.NodeSpec{ProviderID: "aws:///us-west-2a/i-1234567890"},
	}
	kubeClient.CoreV1().Nodes().Create(context.Background(), node, apimachinery_v1.CreateOptions{})

	pod := &v1.Pod{
		ObjectMeta: apimachinery_v1.ObjectMeta{Name: "stuck-pod", Namespace: "default"},
		Spec:       v1.PodSpec{NodeName: "test-node"},
		Status:     v1.PodStatus{Phase: v1.PodRunning},
	}
	kubeClient.CoreV1().Pods("default").Create(context.Background(), pod, apimachinery_v1.CreateOptions{})

	event := &LifecycleEvent{
		AutoScalingGroupName: "my-asg",
		EC2InstanceID:        "i-1234567890",
		LifecycleHookName:    "my-hook",
		referencedNode:       *node,
	}

	err := escalateDrainFailure(event, kubeClient, 2)
	if err == nil {
		t.Fatal("expected escalateDrainFailure to fail when pods never disappear, got nil")
	}
}

func Test_EscalateDrainFailureProductionLikeGraceHappyPath(t *testing.T) {
	t.Log("Test_EscalateDrainFailureProductionLikeGraceHappyPath: max grace 900 with pod TGP 1200 — DELETE caps at 900, fake removes pod immediately so wait succeeds (e2e stubborn-wait is integration-only)")
	kubeClient := fake.NewSimpleClientset()

	node := &v1.Node{
		ObjectMeta: apimachinery_v1.ObjectMeta{Name: "test-node"},
		Spec:       v1.NodeSpec{ProviderID: "aws:///us-west-2a/i-1234567890"},
	}
	kubeClient.CoreV1().Nodes().Create(context.Background(), node, apimachinery_v1.CreateOptions{})

	longGrace := int64(1200)
	pod := &v1.Pod{
		ObjectMeta: apimachinery_v1.ObjectMeta{Name: "long-tgp-pod", Namespace: "default"},
		Spec:       v1.PodSpec{NodeName: "test-node", TerminationGracePeriodSeconds: &longGrace},
		Status:     v1.PodStatus{Phase: v1.PodRunning},
	}
	kubeClient.CoreV1().Pods("default").Create(context.Background(), pod, apimachinery_v1.CreateOptions{})

	event := &LifecycleEvent{
		AutoScalingGroupName: "my-asg",
		EC2InstanceID:        "i-1234567890",
		LifecycleHookName:    "my-hook",
		referencedNode:       *node,
	}

	if err := escalateDrainFailure(event, kubeClient, 900); err != nil {
		t.Fatalf("expected escalation to succeed when API removes pod after delete: %v", err)
	}
}

func Test_WaitForPodsToBeDeletedAlreadyGone(t *testing.T) {
	t.Log("Test_WaitForPodsToBeDeletedAlreadyGone: returns true immediately when no pods exist")
	kubeClient := fake.NewSimpleClientset()
	node := &v1.Node{
		ObjectMeta: apimachinery_v1.ObjectMeta{Name: "test-node"},
		Spec:       v1.NodeSpec{ProviderID: "aws:///us-west-2a/i-1234567890"},
	}
	kubeClient.CoreV1().Nodes().Create(context.Background(), node, apimachinery_v1.CreateOptions{})

	if !waitForPodsToBeDeleted(kubeClient, "test-node", 1*time.Second) {
		t.Fatal("expected waitForPodsToBeDeleted to return true when there are no active pods")
	}
}

func Test_WaitForPodsToBeDeletedTimeout(t *testing.T) {
	t.Log("Test_WaitForPodsToBeDeletedTimeout: returns false when a pod is still present after the timeout")
	kubeClient := fake.NewSimpleClientset()
	node := &v1.Node{
		ObjectMeta: apimachinery_v1.ObjectMeta{Name: "test-node"},
		Spec:       v1.NodeSpec{ProviderID: "aws:///us-west-2a/i-1234567890"},
	}
	kubeClient.CoreV1().Nodes().Create(context.Background(), node, apimachinery_v1.CreateOptions{})
	pod := &v1.Pod{
		ObjectMeta: apimachinery_v1.ObjectMeta{Name: "stuck-pod", Namespace: "default"},
		Spec:       v1.PodSpec{NodeName: "test-node"},
	}
	kubeClient.CoreV1().Pods("default").Create(context.Background(), pod, apimachinery_v1.CreateOptions{})

	if waitForPodsToBeDeleted(kubeClient, "test-node", 50*time.Millisecond) {
		t.Fatal("expected waitForPodsToBeDeleted to return false while the pod is still present")
	}
}

func Test_WaitForPodsToBeDeletedIgnoresSucceededPods(t *testing.T) {
	t.Log("Test_WaitForPodsToBeDeletedIgnoresSucceededPods: terminal pods are not counted as active")
	kubeClient := fake.NewSimpleClientset()
	node := &v1.Node{
		ObjectMeta: apimachinery_v1.ObjectMeta{Name: "test-node"},
		Spec:       v1.NodeSpec{ProviderID: "aws:///us-west-2a/i-1234567890"},
	}
	kubeClient.CoreV1().Nodes().Create(context.Background(), node, apimachinery_v1.CreateOptions{})
	pod := &v1.Pod{
		ObjectMeta: apimachinery_v1.ObjectMeta{Name: "done-pod", Namespace: "default"},
		Spec:       v1.PodSpec{NodeName: "test-node"},
		Status:     v1.PodStatus{Phase: v1.PodSucceeded},
	}
	kubeClient.CoreV1().Pods("default").Create(context.Background(), pod, apimachinery_v1.CreateOptions{})

	if !waitForPodsToBeDeleted(kubeClient, "test-node", 500*time.Millisecond) {
		t.Fatal("expected waitForPodsToBeDeleted true when only succeeded pods exist on node")
	}
}

func Test_WaitForPodsToBeDeletedZeroTimeoutWithRunningPod(t *testing.T) {
	t.Log("Test_WaitForPodsToBeDeletedZeroTimeoutWithRunningPod: timeout<=0 performs a single check")
	kubeClient := fake.NewSimpleClientset()
	node := &v1.Node{
		ObjectMeta: apimachinery_v1.ObjectMeta{Name: "test-node"},
		Spec:       v1.NodeSpec{ProviderID: "aws:///us-west-2a/i-1234567890"},
	}
	kubeClient.CoreV1().Nodes().Create(context.Background(), node, apimachinery_v1.CreateOptions{})
	pod := &v1.Pod{
		ObjectMeta: apimachinery_v1.ObjectMeta{Name: "running-pod", Namespace: "default"},
		Spec:       v1.PodSpec{NodeName: "test-node"},
		Status:     v1.PodStatus{Phase: v1.PodRunning},
	}
	kubeClient.CoreV1().Pods("default").Create(context.Background(), pod, apimachinery_v1.CreateOptions{})

	if waitForPodsToBeDeleted(kubeClient, "test-node", 0) {
		t.Fatal("expected false for zero timeout while a running pod exists")
	}
}
