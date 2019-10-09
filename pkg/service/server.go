package service

import (
	"reflect"
	"runtime"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go/aws"
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
	mgr.workQueue = append(mgr.workQueue, event)
	mgr.workQueueSync.Unlock()
}

func (mgr *Manager) CompleteEvent(event *LifecycleEvent) {
	var (
		queue     = mgr.authenticator.SQSClient
		asgClient = mgr.authenticator.ScalingGroupClient
		url       = event.queueURL
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
		} else {
			newQueue = append(newQueue, e)
		}
	}
	mgr.workQueueSync.Lock()
	mgr.workQueue = newQueue
	mgr.completedEvents++
	mgr.workQueueSync.Unlock()
}

func (mgr *Manager) FailEvent(err error, event *LifecycleEvent, abandon bool) {
	var (
		auth               = mgr.authenticator
		queue              = auth.SQSClient
		scalingGroupClient = auth.ScalingGroupClient
		url                = event.queueURL
	)

	log.Errorf("event %v has failed processing: %v", event.RequestID, err)
	mgr.failedEvents++

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
		auth  = mgr.authenticator
		queue = auth.SQSClient
		ctx   = &mgr.context
		url   = getQueueURLByName(queue, ctx.QueueName)
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

	err = mgr.Process(event)
	if err != nil {
		mgr.FailEvent(err, event, true)
		return
	}
}

func (mgr *Manager) drainNodeTarget(event *LifecycleEvent) error {
	var (
		ctx           = &mgr.context
		kubectlPath   = mgr.context.KubectlLocalPath
		drainTimeout  = ctx.DrainTimeoutSeconds
		retryInterval = ctx.DrainRetryIntervalSeconds
	)

	err := drainNode(kubectlPath, event.referencedNode.Name, drainTimeout, retryInterval)
	if err != nil {
		return err
	}
	log.Infof("completed drain for node %v", event.referencedNode.Name)
	event.SetDrainCompleted(true)
	return nil
}

func (mgr *Manager) drainLoadbalancerTarget(event *LifecycleEvent) error {
	var (
		kubeClient         = mgr.authenticator.KubernetesClient
		elbClient          = mgr.authenticator.ELBv2Client
		instanceID         = event.EC2InstanceID
		ctx                = &mgr.context
		node               = event.referencedNode
		wg                 sync.WaitGroup
		errChannel         = make(chan error, 1)
		finished           = make(chan bool, 1)
		activeTargetGroups = make(map[string]int64)
	)

	if !ctx.WithDeregister {
		return nil
	}

	// get all target groups
	input := &elbv2.DescribeTargetGroupsInput{}
	targetGroups := []*elbv2.TargetGroup{}
	err := elbClient.DescribeTargetGroupsPages(input, func(page *elbv2.DescribeTargetGroupsOutput, lastPage bool) bool {
		targetGroups = append(targetGroups, page.TargetGroups...)
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

	for _, tg := range targetGroups {
		arn := aws.StringValue(tg.TargetGroupArn)
		// check each target group for matches
		found, port, err := findInstanceInTargetGroup(elbClient, arn, instanceID)
		if err != nil {
			return err
		}

		if !found {
			continue
		}

		activeTargetGroups[arn] = port
	}

	// create goroutine per target group with target match
	wg.Add(len(activeTargetGroups))
	for arn, port := range activeTargetGroups {
		go func(activeARN, instance string, activePort int64) {
			// deregister from alb
			log.Debugf("found %v in target group %v, will deregister", instance, activeARN)
			err = deregisterInstance(elbClient, activeARN, instance, activePort)
			if err != nil {
				errChannel <- err
			}

			// waiter for drain
			log.Infof("starting alb-drain waiter for %v in target-group %v", instance, activeARN)
			err = waitForDeregisterInstance(elbClient, activeARN, instance, activePort)
			if err != nil {
				errChannel <- err
			}
			wg.Done()
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
		kubeClient = mgr.authenticator.KubernetesClient
		asgClient  = mgr.authenticator.ScalingGroupClient
		instanceID = event.EC2InstanceID
	)

	// validate instance-id exists as a node
	node, exists := getNodeByInstance(kubeClient, instanceID)
	if !exists {
		return errors.Errorf("instance %v is not seen in cluster nodes", instanceID)
	}
	event.SetReferencedNode(node)

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
