package service

import (
	"context"
	"fmt"
	"math/rand"
	"runtime"
	"strings"
	"time"

	v1 "k8s.io/api/core/v1"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
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
	// InProgressAnnotationKey is the annotation key for setting the state of a node to in-progress
	InProgressAnnotationKey = "lifecycle-manager.keikoproj.io/in-progress"
	// ThreadJitterRangeSeconds configures the jitter range in seconds 0 to N per handler goroutine
	ThreadJitterRangeSeconds = 30.0
	// IterationJitterRangeSeconds configures the jitter range in seconds 0 to N per call iteration goroutine
	IterationJitterRangeSeconds = 1.5
	// NodeAgeCacheTTL defines a node age in minutes for which all caches are flushed
	NodeAgeCacheTTL = 90
	// WaiterMinDelay defines the minimum delay of the IEB waiter
	WaiterMinDelay time.Duration = 10 * time.Second
	// WaiterMaxDelay defines the maximum delay of the IEB waiter
	WaiterMaxDelay time.Duration = 90 * time.Second
	// WaiterMaxAttempts defines the maximum attempts of the IEB waiter
	WaiterMaxAttempts uint32 = 120
)

// Start starts the lifecycle-manager service
func (mgr *Manager) Start() {
	var (
		ctx      = &mgr.context
		metrics  = mgr.metrics
		kube     = mgr.authenticator.KubernetesClient
		auth     = mgr.authenticator
		queueURL = getQueueURLByName(auth.SQSClient, ctx.QueueName)
	)

	log.Infof("starting lifecycle-manager service v%v", version.Version)
	log.Infof("region = %v", ctx.Region)
	log.Infof("queue = %v", ctx.QueueName)
	log.Infof("polling interval seconds = %v", ctx.PollingIntervalSeconds)
	log.Infof("node drain timeout seconds = %v", ctx.DrainTimeoutSeconds)
	log.Infof("unknown node drain timeout seconds = %v", ctx.DrainTimeoutUnknownSeconds)
	log.Infof("node drain retry interval seconds = %v", ctx.DrainRetryIntervalSeconds)
	log.Infof("with alb deregister = %v", ctx.WithDeregister)

	// start metrics server
	log.Infof("starting metrics server on %v%v", MetricsEndpoint, MetricsPort)
	go metrics.Start()

	// restore in-progress events if crashed
	inProgressEvents, err := getNodesByAnnotationKey(kube, InProgressAnnotationKey)
	if err != nil {
		log.Errorf("failed to resume in progress events: %v", err)
	}

	// messages from in-progress are loaded to stream first
	for node, sqsMessage := range inProgressEvents {
		if sqsMessage == "" {
			continue
		}
		log.Infof("trying to resume termination of node/%v", node)
		message, err := deserializeMessage(sqsMessage)
		if err != nil {
			log.Errorf("failed to resume in progress events: %v", err)
		}

		event, err := mgr.newEvent(message, queueURL)
		if err != nil {
			mgr.RejectEvent(err, event)
			continue
		}

		go mgr.Process(event)
	}

	// start SQS poller to load messages to stream from SQS
	go mgr.newPoller()

	// process events from stream
	for message := range mgr.eventStream {

		event, err := mgr.newEvent(message, queueURL)
		if err != nil {
			mgr.RejectEvent(err, event)
			continue
		}

		go mgr.Process(event)
	}
}

func (mgr *Manager) newEvent(message *sqs.Message, queueURL string) (*LifecycleEvent, error) {
	event, err := readMessage(message, queueURL)
	if err != nil {
		return &LifecycleEvent{}, err
	}

	if err = mgr.validateEvent(event); err != nil {
		return event, err
	}

	return event, nil
}

func (mgr *Manager) validateEvent(e *LifecycleEvent) error {
	var (
		auth       = mgr.authenticator
		kubeClient = auth.KubernetesClient
	)

	if e.LifecycleTransition != TerminationEventName {
		return errors.Errorf("got unsupported event type: '%+v'", e.LifecycleTransition)
	}

	if e.EC2InstanceID == "" {
		return errors.Errorf("instance-id not provided in event: %+v", e)
	}

	if e.LifecycleHookName == "" {
		return errors.Errorf("hook-name not provided in event: %+v", e)
	}

	if mgr.EventInQueue(e) {
		return errors.New("event already exists in queue")
	}

	node, exists := getNodeByInstance(kubeClient, e.EC2InstanceID)
	if !exists {
		return errors.Errorf("instance %v is not seen in cluster nodes", e.EC2InstanceID)
	}

	heartbeatInterval, err := getHookHeartbeatInterval(auth.ScalingGroupClient, e.LifecycleHookName, e.AutoScalingGroupName)
	if err != nil {
		return errors.Wrap(err, "failed to get hook heartbeat interval")
	}

	e.SetHeartbeatInterval(heartbeatInterval)
	e.SetReferencedNode(node)

	return nil
}

// Process processes a received event
func (mgr *Manager) Process(event *LifecycleEvent) {

	// add event to work queue
	mgr.AddEvent(event)

	log.Infof("%v> received termination event", event.EC2InstanceID)

	// handle event
	err := mgr.handleEvent(event)
	if err != nil {
		mgr.FailEvent(err, event, true)
		return
	}

	// mark event as completed
	mgr.CompleteEvent(event)
}

func (mgr *Manager) newPoller() {
	var (
		ctx      = &mgr.context
		metrics  = mgr.metrics
		auth     = mgr.authenticator
		stream   = mgr.eventStream
		queue    = auth.SQSClient
		url      = getQueueURLByName(queue, ctx.QueueName)
		interval = ctx.PollingIntervalSeconds
	)

	for {
		log.Debugln("polling for messages from queue")
		goroutines := runtime.NumGoroutine()
		metrics.SetGauge(ActiveGoroutinesMetric, float64(goroutines))
		log.Debugf("active goroutines: %v", goroutines)

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

func (mgr *Manager) drainNodeTarget(event *LifecycleEvent) error {
	var (
		ctx           = &mgr.context
		kubeClient    = mgr.authenticator.KubernetesClient
		kubectlPath   = mgr.context.KubectlLocalPath
		metrics       = mgr.metrics
		drainTimeout  = ctx.DrainTimeoutSeconds
		retryInterval = ctx.DrainRetryIntervalSeconds
		successMsg    = fmt.Sprintf(EventMessageNodeDrainSucceeded, event.referencedNode.Name)
	)

	log.Debugf("%v> acquired drain semaphore", event.EC2InstanceID)
	defer func() {
		mgr.context.MaxDrainConcurrency.Release(1)
		log.Debugf("%v> released drain semaphore", event.EC2InstanceID)
	}()

	metrics.IncGauge(DrainingInstancesCountMetric)
	defer metrics.DecGauge(DrainingInstancesCountMetric)

	if isNodeStatusInCondition(event.referencedNode, v1.ConditionUnknown) {
		log.Infof("%v> node is in unknown state, setting drain deadline to %vs", event.EC2InstanceID, ctx.DrainTimeoutUnknownSeconds)
		drainTimeout = ctx.DrainTimeoutUnknownSeconds
	}

	log.Infof("%v> draining node/%v", event.EC2InstanceID, event.referencedNode.Name)
	err := drainNode(kubectlPath, event.referencedNode.Name, drainTimeout, retryInterval)
	if err != nil {
		metrics.AddCounter(FailedNodeDrainTotalMetric, 1)
		failMsg := fmt.Sprintf(EventMessageNodeDrainFailed, event.referencedNode.Name, err)
		kEvent := newKubernetesEvent(EventMessageNodeDrainFailed, getMessageFields(event, failMsg))
		publishKubernetesEvent(kubeClient, kEvent)
		return err
	}
	log.Infof("%v> completed drain for node/%v", event.EC2InstanceID, event.referencedNode.Name)
	event.SetDrainCompleted(true)
	metrics.AddCounter(SuccessfulNodeDrainTotalMetric, 1)

	kEvent := newKubernetesEvent(EventReasonNodeDrainSucceeded, getMessageFields(event, successMsg))
	publishKubernetesEvent(kubeClient, kEvent)
	return nil
}

func (mgr *Manager) scanMembership(event *LifecycleEvent) (*ScanResult, error) {
	var (
		elbv2Client         = mgr.authenticator.ELBv2Client
		elbClient           = mgr.authenticator.ELBClient
		instanceID          = event.EC2InstanceID
		activeTargetGroups  = make(map[string]int64)
		activeLoadBalancers = make([]string, 0)
		scanResult          = &ScanResult{}
	)

	// get all target groups
	targetGroups := []*elbv2.TargetGroup{}
	err := elbv2Client.DescribeTargetGroupsPages(&elbv2.DescribeTargetGroupsInput{}, func(page *elbv2.DescribeTargetGroupsOutput, lastPage bool) bool {
		targetGroups = append(targetGroups, page.TargetGroups...)
		return page.NextMarker != nil
	})
	if err != nil {
		return scanResult, err
	}

	// get all classic elbs
	elbDescriptions := []*elb.LoadBalancerDescription{}
	err = elbClient.DescribeLoadBalancersPages(&elb.DescribeLoadBalancersInput{}, func(page *elb.DescribeLoadBalancersOutput, lastPage bool) bool {
		elbDescriptions = append(elbDescriptions, page.LoadBalancerDescriptions...)
		return page.NextMarker != nil
	})
	if err != nil {
		return scanResult, err
	}

	log.Infof("%v> checking targetgroup/elb membership", instanceID)
	// find instance in target groups
	for i, tg := range targetGroups {
		arn := aws.StringValue(tg.TargetGroupArn)
		// check each target group for matches
		waitJitter(IterationJitterRangeSeconds)
		log.Debugf("%v> checking membership in %v (%v/%v)", instanceID, arn, i, len(targetGroups))
		found, port, err := findInstanceInTargetGroup(elbv2Client, arn, instanceID)
		if err != nil {
			if awsErr, ok := err.(awserr.Error); ok {
				if awsErr.Code() == elbv2.ErrCodeTargetGroupNotFoundException {
					log.Warnf("%v> target group %v not found, skipping", instanceID, arn)
					continue
				}
			}
			return scanResult, err
		}

		if !found {
			continue
		}
		activeTargetGroups[arn] = port
		mgr.AddTargetByInstance(arn, mgr.NewTarget(arn, instanceID, port, TargetTypeTargetGroup))
	}
	scanResult.ActiveTargetGroups = activeTargetGroups

	// find instance in classic elbs
	for i, desc := range elbDescriptions {
		elbName := aws.StringValue(desc.LoadBalancerName)
		// check each target group for matches
		waitJitter(IterationJitterRangeSeconds)
		log.Debugf("%v> checking membership in %v (%v/%v)", instanceID, elbName, i, len(elbDescriptions))
		found, err := findInstanceInClassicBalancer(elbClient, elbName, instanceID)
		if err != nil {
			if awsErr, ok := err.(awserr.Error); ok {
				if awsErr.Code() == elb.ErrCodeAccessPointNotFoundException {
					log.Warnf("%v> classic-elb %v not found, skipping", instanceID, elbName)
					continue
				}
			}
			return scanResult, err
		}

		if !found {
			continue
		}
		mgr.AddTargetByInstance(elbName, mgr.NewTarget(elbName, instanceID, 0, TargetTypeClassicELB))
		activeLoadBalancers = append(activeLoadBalancers, elbName)
	}
	scanResult.ActiveLoadBalancers = activeLoadBalancers

	log.Infof("%v> found %v target groups & %v classic-elb", instanceID, len(activeTargetGroups), len(elbDescriptions))
	return scanResult, nil
}

func (mgr *Manager) executeDeregisterWaiters(event *LifecycleEvent, scanResult *ScanResult, waiter *Waiter) {
	var (
		kubeClient      = mgr.authenticator.KubernetesClient
		elbv2Client     = mgr.authenticator.ELBv2Client
		elbClient       = mgr.authenticator.ELBClient
		instanceID      = event.EC2InstanceID
		metrics         = mgr.metrics
		workQueueLength = len(scanResult.ActiveTargetGroups) + len(scanResult.ActiveLoadBalancers)
	)

	waiter.Add(workQueueLength)
	// spawn waiters for classic elb
	for _, elbName := range scanResult.ActiveLoadBalancers {
		go func(elbName, instance string) {
			waiter.IncClassicWaiter()
			defer waiter.DecClassicWaiter()
			defer waiter.Done()

			// wait for deregister/drain
			log.Debugf("%v> starting classic-elb waiter for %v", instance, elbName)
			err := waitForDeregisterInstance(event, elbClient, elbName, instance)
			if err != nil {
				if awsErr, ok := err.(awserr.Error); ok {
					if awsErr.Code() == elb.ErrCodeAccessPointNotFoundException {
						log.Warnf("%v> classic-elb %v not found, skipping", instance, elbName)
						return
					}
				}
				waiterErr := WaiterError{
					Error: err,
					Type:  TargetTypeClassicELB,
				}
				waiter.errors <- waiterErr
				return
			}

			// publish event
			msg := fmt.Sprintf(EventMessageInstanceDeregisterSucceeded, instance, elbName)
			msgFields := map[string]string{
				"elbName":       elbName,
				"ec2InstanceId": instance,
				"elbType":       "classic-elb",
				"details":       msg,
			}
			publishKubernetesEvent(kubeClient, newKubernetesEvent(EventReasonInstanceDeregisterSucceeded, msgFields))
			metrics.AddCounter(SuccessfulLBDeregisterTotalMetric, 1)
		}(elbName, instanceID)
	}

	// spawn waiters for target groups
	for arn, port := range scanResult.ActiveTargetGroups {
		go func(activeARN, instance string, activePort int64) {
			waiter.IncTargetGroupWaiter()
			defer waiter.DecTargetGroupWaiter()
			defer waiter.Done()
			// wait for deregister/drain
			log.Debugf("%v> starting target group waiter for %v", instance, activeARN)
			err := waitForDeregisterTarget(event, elbv2Client, activeARN, instance, activePort)
			if err != nil {
				if awsErr, ok := err.(awserr.Error); ok {
					if awsErr.Code() == elbv2.ErrCodeTargetGroupNotFoundException {
						log.Warnf("%v> target group %v not found, skipping", instance, activeARN)
						return
					}
				}
				waiterErr := WaiterError{
					Error: err,
					Type:  TargetTypeTargetGroup,
				}
				waiter.errors <- waiterErr
				return
			}

			// publish event
			msg := fmt.Sprintf(EventMessageTargetDeregisterSucceeded, instance, activePort, activeARN)
			msgFields := map[string]string{
				"port":          fmt.Sprintf("%d", activePort),
				"targetGroup":   activeARN,
				"ec2InstanceId": instance,
				"elbType":       "alb",
				"details":       msg,
			}
			publishKubernetesEvent(kubeClient, newKubernetesEvent(EventReasonTargetDeregisterSucceeded, msgFields))
			metrics.AddCounter(SuccessfulLBDeregisterTotalMetric, 1)
		}(arn, instanceID, port)
	}

	go func() {
		for {
			select {
			case <-waiter.finished:
				return
			default:
				log.Infof("%v> there are %v pending classic-elb waiters", event.EC2InstanceID, waiter.classicWaiterCount)
				log.Infof("%v> there are %v pending target-group waiters", event.EC2InstanceID, waiter.targetGroupWaiterCount)
				time.Sleep(180 * time.Second)
			}
		}
	}()

	waiter.Wait()
	close(waiter.finished)
}

func (mgr *Manager) drainLoadbalancerTarget(event *LifecycleEvent) error {
	var (
		instanceID = event.EC2InstanceID
		ctx        = &mgr.context
		metrics    = mgr.metrics
		node       = event.referencedNode
		kubeClient = mgr.authenticator.KubernetesClient
		errs       error
		isFinished bool
	)

	if !ctx.WithDeregister {
		return nil
	}
	log.Infof("%v> starting load balancer drain worker", instanceID)

	metrics.IncGauge(DeregisteringInstancesCountMetric)
	defer metrics.DecGauge(DeregisteringInstancesCountMetric)

	// add exclusion label
	log.Debugf("%v> excluding node %v from load balancers", instanceID, node.Name)
	err := labelNode(ctx.KubectlLocalPath, node.Name, ExcludeLabelKey, ExcludeLabelValue)
	if err != nil {
		return err
	}

	now := time.Now().UTC()
	nodeCreationTime := node.CreationTimestamp.UTC()
	nodeAge := int(now.Sub(nodeCreationTime).Minutes())
	if nodeAge <= NodeAgeCacheTTL {
		log.Warnf("%v> node younger than %vm was terminated, flushing caches", instanceID, NodeAgeCacheTTL)
		mgr.context.CacheConfig.FlushCache("elasticloadbalancing.DescribeTargetHealth")
		mgr.context.CacheConfig.FlushCache("elasticloadbalancing.DescribeInstanceHealth")
	}

	// scan and update targets
	log.Infof("%v> scanner starting", instanceID)
	scanResults, err := mgr.scanMembership(event)
	if err != nil {
		return err
	}

	waitJitter(ThreadJitterRangeSeconds)

	// trigger deregistrator to start scanning
	log.Infof("%v> queuing deregistrator", instanceID)
	deregistrator := &Deregistrator{
		errors: make(chan DeregistrationError, 0),
	}
	go mgr.startDeregistrator(deregistrator)

	// create waiters
	log.Infof("%v> queuing waiters", instanceID)
	waiter := &Waiter{
		finished: make(chan bool),
		errors:   make(chan WaiterError, 0),
	}
	go mgr.executeDeregisterWaiters(event, scanResults, waiter)

	for {

		if isFinished {
			break
		}

		select {
		case <-waiter.finished:
			isFinished = true
		case err := <-deregistrator.errors:
			var msgFields map[string]string
			switch err.Type {
			case TargetTypeClassicELB:
				msg := fmt.Sprintf(EventMessageInstanceDeregisterFailed, err.Instances, err.Target, err)
				msgFields = map[string]string{
					"elbName":       err.Target,
					"ec2InstanceId": strings.Join(err.Instances, ","),
					"elbType":       TargetTypeClassicELB.String(),
					"details":       msg,
				}
			case TargetTypeTargetGroup:
				msg := fmt.Sprintf(EventMessageTargetDeregisterFailed, err.Instances, err.Target, err)
				msgFields = map[string]string{
					"targetGroup":   err.Target,
					"ec2InstanceId": strings.Join(err.Instances, ","),
					"elbType":       TargetTypeTargetGroup.String(),
					"details":       msg,
				}
			}
			publishKubernetesEvent(kubeClient, newKubernetesEvent(EventReasonInstanceDeregisterFailed, msgFields))
			errs = errors.Wrap(err.Error, "deregister failed")
			metrics.AddCounter(FailedLBDeregisterTotalMetric, 1)
		case err := <-waiter.errors:
			if err.Error != nil {
				errs = errors.Wrap(err.Error, "waiter failed")
			}
		}
	}

	if errs != nil {
		return errs
	}

	log.Debugf("%v> successfully executed all drainLoadbalancerTarget goroutines", instanceID)
	event.SetDeregisterCompleted(true)
	return nil
}

func (mgr *Manager) handleEvent(event *LifecycleEvent) error {
	var (
		asgClient = mgr.authenticator.ScalingGroupClient
		errs      error
	)

	// send heartbeat at intervals
	go sendHeartbeat(asgClient, event)

	// Annotate node with InProgressAnnotationKey = EventBody for resuming in case of crash
	storeMessage, err := serializeMessage(event.message)
	if err != nil {
		log.Errorf("%v> failed to serialize message for storage, event cannot be restored", event.EC2InstanceID)
	} else {
		annotateNode(mgr.context.KubectlLocalPath, event.referencedNode.Name, InProgressAnnotationKey, string(storeMessage))
	}

	// acquire a semaphore to drain the node, allow up to mgr.maxDrainConcurrency drains in parallel
	if err := mgr.context.MaxDrainConcurrency.Acquire(context.Background(), 1); err != nil {
		return err
	}
	err = mgr.drainNodeTarget(event)
	if err != nil {
		errs = errors.Wrap(err, "failed to drain node")
	}

	// alb-drain action
	err = mgr.drainLoadbalancerTarget(event)
	if err != nil {
		errs = errors.Wrap(err, "failed to deregister load balancers")
	}

	// clear the state annotation once processing is ended
	annotateNode(mgr.context.KubectlLocalPath, event.referencedNode.Name, InProgressAnnotationKey, "")

	if errs != nil {
		return errs
	}

	return nil
}

func waitJitter(max float64) {
	min := 0.5
	rand.Seed(time.Now().UnixNano())
	r := min + rand.Float64()*(max-min)
	log.Debugf("adding jitter of %v seconds to waiter\n", r)
	time.Sleep(time.Duration(r) * time.Second)
}
