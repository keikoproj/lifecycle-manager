package service

import (
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/elbv2"
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
		auth   = mgr.authenticator
		queue  = auth.SQSClient
		ctx    = &mgr.context
		stream = mgr.eventStream
		url    = getQueueURLByName(auth.SQSClient, ctx.QueueName)
	)

	log.Infof("starting lifecycle-manager service v%v", version.Version)
	log.Infof("region = %v", ctx.Region)
	log.Infof("queue = %v", url)
	log.Infof("polling interval seconds = %v", ctx.PollingIntervalSeconds)
	log.Infof("node drain timeout seconds = %v", ctx.DrainTimeoutSeconds)
	log.Infof("node drain retry interval seconds = %v", ctx.DrainRetryIntervalSeconds)
	log.Infof("with alb deregister = %v", ctx.WithDeregister)

	// create a poller goroutine that reads from sqs and posts to channel
	log.Info("spawning sqs poller")
	go newPoller(auth.SQSClient, stream, url, ctx.PollingIntervalSeconds)

	// process messags from channel
	for message := range mgr.eventStream {
		event, err := readMessage(message)
		if err != nil {
			log.Errorf("failed to read message: %v", err)
			continue
		}

		if !event.IsValid() {
			err = deleteMessage(queue, url, event.receiptHandle)
			if err != nil {
				log.Errorf("failed to delete message: %v", err)
			}
			continue
		}

		heartbeatInterval, err := getHookHeartbeatInterval(auth.ScalingGroupClient, event.LifecycleHookName, event.AutoScalingGroupName)
		if err != nil {
			log.Errorf("failed to get hook heartbeat interval: %v", err)
			continue
		}

		event.SetHeartbeatInterval(heartbeatInterval)
		event.SetQueueURL(url)

		// spawn goroutine for processing individual events
		log.Info("spawning event handler")
		go mgr.Process(event)
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
		log.Warnf("abandoning instance '%v', ", event.EC2InstanceID)
		completeLifecycleAction(scalingGroupClient, *event, AbandonAction)
	}

	err = deleteMessage(queue, url, event.receiptHandle)
	if err != nil {
		log.Errorf("failed to delete message: %v", err)
	}
	mgr.CompleteEvent(*event)
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
	log.Infof("completed drain for node '%v'", event.referencedNode.Name)
	event.SetDrainCompleted(true)
	return nil
}

func (mgr *Manager) drainLoadbalancerTarget(event *LifecycleEvent) error {
	var (
		kubeClient = mgr.authenticator.KubernetesClient
		elbClient  = mgr.authenticator.ELBv2Client
		instanceID = event.EC2InstanceID
		ctx        = &mgr.context
		node       = event.referencedNode
	)

	if !ctx.WithDeregister {
		return nil
	}

	// get all target groups
	targetGroups, err := elbClient.DescribeTargetGroups(&elbv2.DescribeTargetGroupsInput{})
	if err != nil {
		return err
	}

	for _, tg := range targetGroups.TargetGroups {
		arn := aws.StringValue(tg.TargetGroupArn)

		// check each target group for matches
		found, port, err := findInstanceInTargetGroup(elbClient, arn, instanceID)
		if err != nil {
			return err
		}

		if !found {
			continue
		}

		// add exclusion label
		log.Debugf("excluding node '%v' from load balancers", node.Name)
		err = labelNode(kubeClient, ctx.KubectlLocalPath, node.Name, ExcludeLabelKey, ExcludeLabelValue)
		if err != nil {
			return err
		}

		// deregister from alb
		log.Debugf("found %v in target group %v, will deregister", instanceID, arn)
		err = deregisterInstance(elbClient, arn, instanceID, port)
		if err != nil {
			return err
		}

		// waiter for drain
		log.Infof("%v waiting for alb-drain", arn)
		err = waitForDeregisterInstance(elbClient, arn, instanceID, port)
		if err != nil {
			return err
		}
	}
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
		return errors.Errorf("instance '%v' is not seen in cluster nodes", instanceID)
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
