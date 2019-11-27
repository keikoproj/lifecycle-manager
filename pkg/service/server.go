package service

import (
	"fmt"
	"math/rand"
	"reflect"
	"runtime"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/elb"
	"github.com/aws/aws-sdk-go/service/elbv2"
	"github.com/aws/aws-sdk-go/service/sqs"
	"github.com/keikoproj/lifecycle-manager/pkg/log"
	"github.com/keikoproj/lifecycle-manager/pkg/version"
	"github.com/pkg/errors"
)

var (
	// TerminationEventName is the event name of a terminating lifecycle hook
	TerminationEventName = "autoscaling:EC2_INSTANCE_TERMINATING"
	// ContinueAction is the name of the action in case we are successful in draining
	ContinueAction = "CONTINUE"
	// AbandonAction is the name of the action in case we are unsuccessful in draining
	AbandonAction = "ABANDON"
	// ExcludeLabelKey is the alb-ingress-controller exclude label key
	ExcludeLabelKey = "alpha.service-controller.kubernetes.io/exclude-balancer"
	// ExcludeLabelValue is the alb-ingress-controller exclude label value
	ExcludeLabelValue = "true"
	// ThreadJitterRangeSeconds configures the jitter range in seconds 0 to N per handler goroutine
	ThreadJitterRangeSeconds = 180
	// DeregisterJitterRangeSeconds configures the jitter range in seconds 0 to N per ELB/ALB deregister goroutine
	DeregisterJitterRangeSeconds = 30
)

// Start starts the lifecycle-manager service
func (mgr *Manager) Start() {
	var (
		ctx = &mgr.context
	)

	log.Infof("starting lifecycle-manager service v%v", version.Version)
	log.Infof("region = %v", ctx.Region)
	log.Infof("queue = %v", ctx.QueueName)
	log.Infof("polling interval seconds = %v", ctx.PollingIntervalSeconds)
	log.Infof("node drain timeout seconds = %v", ctx.DrainTimeoutSeconds)
	log.Infof("node drain retry interval seconds = %v", ctx.DrainRetryIntervalSeconds)
	log.Infof("with alb deregister = %v", ctx.WithDeregister)

	// create a poller goroutine that reads from sqs and posts to channel
	log.Info("spawning sqs poller")
	go mgr.newPoller()

	// process messags from channel
	for message := range mgr.eventStream {
		go mgr.newWorker(message)
	}
}

// Process processes a received event
func (mgr *Manager) Process(event *LifecycleEvent) error {

	// add event to work queue
	mgr.AddEvent(event)

	// handle event
	err := mgr.handleEvent(event)
	if err != nil {
		return err
	}

	// mark event as completed
	mgr.CompleteEvent(event)

	return nil
}

func (mgr *Manager) AddEvent(event *LifecycleEvent) {
	mgr.workQueueSync.Lock()
	event.SetEventTimeStarted(time.Now())
	mgr.workQueue = append(mgr.workQueue, event)
	mgr.workQueueSync.Unlock()
}

func (mgr *Manager) CompleteEvent(event *LifecycleEvent) {
	var (
		queue      = mgr.authenticator.SQSClient
		kubeClient = mgr.authenticator.KubernetesClient
		asgClient  = mgr.authenticator.ScalingGroupClient
		url        = event.queueURL
		t          = time.Since(event.startTime).Seconds()
	)
	newQueue := make([]*LifecycleEvent, 0)
	for _, e := range mgr.workQueue {
		if reflect.DeepEqual(event, e) {
			// found event in work queue
			log.Infof("event %v completed processing", event.RequestID)
			event.SetEventCompleted(true)

			err := deleteMessage(queue, url, event.receiptHandle)
			if err != nil {
				log.Errorf("failed to delete message: %v", err)
			}
			err = completeLifecycleAction(asgClient, *event, ContinueAction)
			if err != nil {
				log.Errorf("failed to complete lifecycle action: %v", err)
			}
			msg := fmt.Sprintf(EventMessageLifecycleHookProcessed, event.RequestID, event.EC2InstanceID, t)
			publishKubernetesEvent(kubeClient, newKubernetesEvent(EventReasonLifecycleHookProcessed, msg, event.referencedNode.Name))
		} else {
			newQueue = append(newQueue, e)
		}
	}
	mgr.workQueueSync.Lock()
	mgr.workQueue = newQueue
	mgr.completedEvents++
	log.Infof("event %v for instance %v completed after %vs", event.RequestID, event.EC2InstanceID, t)
	mgr.workQueueSync.Unlock()
}

func (mgr *Manager) FailEvent(err error, event *LifecycleEvent, abandon bool) {
	var (
		auth               = mgr.authenticator
		kubeClient         = auth.KubernetesClient
		queue              = auth.SQSClient
		scalingGroupClient = auth.ScalingGroupClient
		url                = event.queueURL
		t                  = time.Since(event.startTime).Seconds()
	)
	log.Errorf("event %v has failed processing after %vs: %v", event.RequestID, t, err)
	mgr.failedEvents++
	msg := fmt.Sprintf(EventMessageLifecycleHookFailed, event.RequestID, t, err)
	publishKubernetesEvent(kubeClient, newKubernetesEvent(EventReasonLifecycleHookFailed, msg, event.referencedNode.Name))

	if abandon {
		log.Warnf("abandoning instance %v", event.EC2InstanceID)
		completeLifecycleAction(scalingGroupClient, *event, AbandonAction)
	}

	if reflect.DeepEqual(event, LifecycleEvent{}) {
		log.Errorf("event failed: invalid message: %v", err)
		return
	}

	err = deleteMessage(queue, url, event.receiptHandle)
	if err != nil {
		log.Errorf("event failed: failed to delete message: %v", err)
	}

}

func (mgr *Manager) RejectEvent(err error, event *LifecycleEvent) {
	var (
		auth  = mgr.authenticator
		queue = auth.SQSClient
		url   = event.queueURL
	)

	log.Debugf("event %v has been rejected for processing: %v", event.RequestID, err)
	mgr.rejectedEvents++

	if reflect.DeepEqual(event, LifecycleEvent{}) {
		log.Errorf("event failed: invalid message: %v", err)
		return
	}

	err = deleteMessage(queue, url, event.receiptHandle)
	if err != nil {
		log.Errorf("event failed: failed to delete message: %v", err)
	}
}

func (mgr *Manager) newPoller() {
	var (
		ctx      = &mgr.context
		auth     = mgr.authenticator
		stream   = mgr.eventStream
		queue    = auth.SQSClient
		url      = getQueueURLByName(queue, ctx.QueueName)
		interval = ctx.PollingIntervalSeconds
	)

	for {
		log.Debugln("polling for messages from queue")
		log.Debugf("active goroutines: %v", runtime.NumGoroutine())
		output, err := queue.ReceiveMessage(&sqs.ReceiveMessageInput{
			QueueUrl: aws.String(url),
			AttributeNames: aws.StringSlice([]string{
				"SenderId",
			}),
			MaxNumberOfMessages: aws.Int64(1),
			WaitTimeSeconds:     aws.Int64(interval),
		})
		if err != nil {
			log.Errorf("unable to receive message from queue %s, %v.", url, err)
			time.Sleep(time.Duration(interval) * time.Second)
		}
		if len(output.Messages) == 0 {
			log.Debugln("no messages received in interval")
		}
		for _, message := range output.Messages {
			stream <- message
		}
	}
}

func (mgr *Manager) newWorker(message *sqs.Message) {
	var (
		auth       = mgr.authenticator
		kubeClient = auth.KubernetesClient
		queue      = auth.SQSClient
		ctx        = &mgr.context
		url        = getQueueURLByName(queue, ctx.QueueName)
	)

	// process messags from channel
	event, err := readMessage(message)
	if err != nil {
		err = errors.Wrap(err, "failed to read message")
		mgr.RejectEvent(err, event)
		return
	}
	event.SetQueueURL(url)

	if !event.IsValid() {
		err = errors.Wrap(err, "received invalid event")
		mgr.RejectEvent(err, event)
		return
	}

	if event.IsAlreadyExist(mgr.workQueue) {
		err := errors.New("event already exists")
		mgr.RejectEvent(err, event)
		return
	}

	heartbeatInterval, err := getHookHeartbeatInterval(auth.ScalingGroupClient, event.LifecycleHookName, event.AutoScalingGroupName)
	if err != nil {
		err = errors.Wrap(err, "failed to get hook heartbeat interval")
		mgr.RejectEvent(err, event)
		return
	}
	event.SetHeartbeatInterval(heartbeatInterval)

	node, exists := getNodeByInstance(kubeClient, event.EC2InstanceID)
	if !exists {
		err = errors.Errorf("instance %v is not seen in cluster nodes", event.EC2InstanceID)
		mgr.RejectEvent(err, event)
		return
	}
	event.SetReferencedNode(node)

	msg := fmt.Sprintf(EventMessageLifecycleHookReceived, event.RequestID, event.EC2InstanceID)
	publishKubernetesEvent(kubeClient, newKubernetesEvent(EventReasonLifecycleHookReceived, msg, event.referencedNode.Name))

	err = mgr.Process(event)
	if err != nil {
		mgr.FailEvent(err, event, true)
		return
	}
}

func (mgr *Manager) drainNodeTarget(event *LifecycleEvent) error {
	var (
		ctx           = &mgr.context
		kubeClient    = mgr.authenticator.KubernetesClient
		kubectlPath   = mgr.context.KubectlLocalPath
		drainTimeout  = ctx.DrainTimeoutSeconds
		retryInterval = ctx.DrainRetryIntervalSeconds
		successMsg    = fmt.Sprintf(EventMessageNodeDrainSucceeded, event.referencedNode.Name)
	)

	err := drainNode(kubectlPath, event.referencedNode.Name, drainTimeout, retryInterval)
	if err != nil {
		failMsg := fmt.Sprintf(EventMessageNodeDrainFailed, event.referencedNode.Name, err)
		publishKubernetesEvent(kubeClient, newKubernetesEvent(EventReasonNodeDrainFailed, failMsg, event.referencedNode.Name))
		return err
	}
	log.Infof("completed drain for node %v", event.referencedNode.Name)
	event.SetDrainCompleted(true)

	publishKubernetesEvent(kubeClient, newKubernetesEvent(EventReasonNodeDrainSucceeded, successMsg, event.referencedNode.Name))
	return nil
}

func (mgr *Manager) drainLoadbalancerTarget(event *LifecycleEvent) error {
	var (
		kubeClient          = mgr.authenticator.KubernetesClient
		elbv2Client         = mgr.authenticator.ELBv2Client
		elbClient           = mgr.authenticator.ELBClient
		instanceID          = event.EC2InstanceID
		ctx                 = &mgr.context
		node                = event.referencedNode
		wg                  sync.WaitGroup
		errChannel          = make(chan error, 1)
		finished            = make(chan bool, 1)
		activeTargetGroups  = make(map[string]int64)
		activeLoadBalancers = make([]string, 0)
	)

	if !ctx.WithDeregister {
		return nil
	}

	// get all target groups
	input := &elbv2.DescribeTargetGroupsInput{}
	targetGroups := []*elbv2.TargetGroup{}
	err := elbv2Client.DescribeTargetGroupsPages(input, func(page *elbv2.DescribeTargetGroupsOutput, lastPage bool) bool {
		targetGroups = append(targetGroups, page.TargetGroups...)
		return page.NextMarker != nil
	})
	if err != nil {
		return err
	}

	// get all classic elbs
	elbDescriptions := []*elb.LoadBalancerDescription{}
	err = elbClient.DescribeLoadBalancersPages(&elb.DescribeLoadBalancersInput{}, func(page *elb.DescribeLoadBalancersOutput, lastPage bool) bool {
		elbDescriptions = append(elbDescriptions, page.LoadBalancerDescriptions...)
		return page.NextMarker != nil
	})
	if err != nil {
		return err
	}

	// add exclusion label
	log.Debugf("excluding node %v from load balancers", node.Name)
	err = labelNode(kubeClient, ctx.KubectlLocalPath, node.Name, ExcludeLabelKey, ExcludeLabelValue)
	if err != nil {
		return err
	}

	// find instance in target groups
	for _, tg := range targetGroups {
		arn := aws.StringValue(tg.TargetGroupArn)
		// check each target group for matches
		found, port, err := findInstanceInTargetGroup(elbv2Client, arn, instanceID)
		if err != nil {
			return err
		}

		if !found {
			continue
		}
		activeTargetGroups[arn] = port
	}

	// find instance in classic elbs
	for _, desc := range elbDescriptions {
		elbName := aws.StringValue(desc.LoadBalancerName)
		// check each target group for matches
		found, err := findInstanceInClassicBalancer(elbClient, elbName, instanceID)
		if err != nil {
			return err
		}

		if !found {
			continue
		}
		activeLoadBalancers = append(activeLoadBalancers, elbName)
	}

	workQueueLength := len(activeTargetGroups) + len(activeLoadBalancers)

	// create goroutine per target group with target match
	wg.Add(workQueueLength)

	// sleep for random 0-180 seconds to goroutine
	waitJitter(ThreadJitterRangeSeconds)

	// handle classic load balancers
	for _, elbName := range activeLoadBalancers {

		// sleep for random jitter between iterations of deregister
		waitJitter(DeregisterJitterRangeSeconds)

		go func(elbName, instance string) {
			defer wg.Done()
			// deregister from classic-elb
			log.Debugf("found %v in classic-elb %v, will deregister", instance, elbName)
			err = deregisterInstance(elbClient, elbName, instance)
			if err != nil {
				errChannel <- err
				msg := fmt.Sprintf(EventMessageInstanceDeregisterFailed, instance, elbName, err)
				publishKubernetesEvent(kubeClient, newKubernetesEvent(EventReasonInstanceDeregisterFailed, msg, event.referencedNode.Name))
				return
			}

			// wait for deregister/drain
			log.Infof("starting elb-drain waiter for %v in classic-elb %v", instance, elbName)
			err = waitForDeregisterInstance(elbClient, elbName, instance)
			if err != nil {
				errChannel <- err
				msg := fmt.Sprintf(EventMessageInstanceDeregisterFailed, instance, elbName, err)
				publishKubernetesEvent(kubeClient, newKubernetesEvent(EventReasonInstanceDeregisterFailed, msg, event.referencedNode.Name))
				return
			}

			// publish event
			msg := fmt.Sprintf(EventMessageInstanceDeregisterSucceeded, instance, elbName)
			publishKubernetesEvent(kubeClient, newKubernetesEvent(EventReasonInstanceDeregisterSucceeded, msg, event.referencedNode.Name))
		}(elbName, instanceID)
	}
	// handle v2 load balancers / target groups
	for arn, port := range activeTargetGroups {

		// sleep for random jitter between iterations of deregister
		waitJitter(DeregisterJitterRangeSeconds)

		go func(activeARN, instance string, activePort int64) {
			defer wg.Done()
			// deregister from alb
			log.Debugf("found %v in target group %v, will deregister", instance, activeARN)
			err = deregisterTarget(elbv2Client, activeARN, instance, activePort)
			if err != nil {
				errChannel <- err
				msg := fmt.Sprintf(EventMessageTargetDeregisterFailed, instance, activePort, activeARN, err)
				publishKubernetesEvent(kubeClient, newKubernetesEvent(EventReasonTargetDeregisterFailed, msg, event.referencedNode.Name))
				return
			}

			// wait for deregister/drain
			log.Infof("starting alb-drain waiter for %v in target-group %v", instance, activeARN)
			err = waitForDeregisterTarget(elbv2Client, activeARN, instance, activePort)
			if err != nil {
				errChannel <- err
				msg := fmt.Sprintf(EventMessageTargetDeregisterFailed, instance, activePort, activeARN, err)
				publishKubernetesEvent(kubeClient, newKubernetesEvent(EventReasonTargetDeregisterFailed, msg, event.referencedNode.Name))
				return
			}

			// publish event
			msg := fmt.Sprintf(EventMessageTargetDeregisterSucceeded, instance, activePort, activeARN)
			publishKubernetesEvent(kubeClient, newKubernetesEvent(EventReasonTargetDeregisterSucceeded, msg, event.referencedNode.Name))
		}(arn, instanceID, port)
	}

	// wait indefinitely for goroutines to complete
	go func() {
		wg.Wait()
		close(finished)
	}()

	var errs error
	select {
	case <-finished:
	case err := <-errChannel:
		if err != nil {
			errs = errors.Wrap(err, "failed to process alb-drain: ")
		}
	}

	if errs != nil {
		return errs
	}

	log.Debugf("successfully executed all drainLoadbalancerTarget goroutines")
	event.SetDeregisterCompleted(true)
	return nil
}

func (mgr *Manager) handleEvent(event *LifecycleEvent) error {
	var (
		asgClient = mgr.authenticator.ScalingGroupClient
	)

	// send heartbeat at intervals
	go sendHeartbeat(asgClient, event)

	// node drain
	err := mgr.drainNodeTarget(event)
	if err != nil {
		return err
	}

	// alb-drain action
	err = mgr.drainLoadbalancerTarget(event)
	if err != nil {
		return err
	}

	return nil
}

func waitJitter(rangeSeconds int) {
	rand.Seed(time.Now().UnixNano())
	n := rand.Intn(rangeSeconds)
	log.Infof("adding jitter of %d seconds to waiter", n)
	time.Sleep(time.Duration(n) * time.Second)
}
