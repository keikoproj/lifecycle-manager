package service

import (
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
func (mgr *Manager) Process(event *LifecycleEvent) {
	var (
		queue              = mgr.authenticator.SQSClient
		scalingGroupClient = mgr.authenticator.ScalingGroupClient
		url                = event.queueURL
	)
	if event.IsAlreadyExist(mgr.queue) {
		err := deleteMessage(queue, url, event.receiptHandle)
		if err != nil {
			log.Errorf("failed to delete message: %v", err)
		}
		return
	}
	mgr.AddEvent(*event)
	err := mgr.handleEvent(event)
	if err != nil {
		log.Errorf("failed to process event: %v", err)
		log.Warnf("abandoning instance %v, ", event.EC2InstanceID)
		completeLifecycleAction(scalingGroupClient, *event, AbandonAction)
	}
	err = deleteMessage(queue, url, event.receiptHandle)
	if err != nil {
		log.Errorf("failed to delete message: %v", err)
	}
	mgr.CompleteEvent(*event)
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
		url   = getQueueURLByName(auth.SQSClient, ctx.QueueName)
	)

	// process messags from channel
	event, err := readMessage(message)
	if err != nil {
		log.Errorf("failed to read message: %v", err)
		return
	}
	if !event.IsValid() {
		err = deleteMessage(queue, url, event.receiptHandle)
		if err != nil {
			log.Errorf("failed to delete message: %v", err)
		}
		return
	}
	heartbeatInterval, err := getHookHeartbeatInterval(auth.ScalingGroupClient, event.LifecycleHookName, event.AutoScalingGroupName)
	if err != nil {
		log.Errorf("failed to get hook heartbeat interval: %v", err)
		return
	}
	event.SetHeartbeatInterval(heartbeatInterval)
	event.SetQueueURL(url)

	mgr.Process(event)
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
	// handle classic load balancers
	for _, elbName := range activeLoadBalancers {
		go func(elbName, instance string) {
			// deregister from classic-elb
			log.Debugf("found %v in classic-elb %v, will deregister", instance, elbName)
			err = deregisterInstance(elbClient, elbName, instance)
			if err != nil {
				errChannel <- err
			}

			// wait for deregister/drain
			log.Infof("starting elb-drain waiter for %v in classic-elb %v", instance, elbName)
			err = waitForDeregisterInstance(elbClient, elbName, instance)
			if err != nil {
				errChannel <- err
			}
			wg.Done()
		}(elbName, instanceID)
	}
	// handle v2 load balancers / target groups
	for arn, port := range activeTargetGroups {
		go func(activeARN, instance string, activePort int64) {
			// deregister from alb
			log.Debugf("found %v in target group %v, will deregister", instance, activeARN)
			err = deregisterTarget(elbv2Client, activeARN, instance, activePort)
			if err != nil {
				errChannel <- err
			}

			// wait for deregister/drain
			log.Infof("starting alb-drain waiter for %v in target-group %v", instance, activeARN)
			err = waitForDeregisterTarget(elbv2Client, activeARN, instance, activePort)
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

	// complete lifecycle action
	event.SetEventCompleted(true)
	err = completeLifecycleAction(asgClient, *event, ContinueAction)
	if err != nil {
		return err
	}

	return nil
}
