package service

import (
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
)

// Start starts the lifecycle-manager service
func (g *Manager) Start() {
	var (
		auth   = g.authenticator
		queue  = auth.SQSClient
		ctx    = &g.context
		stream = g.eventStream
		url    = getQueueURLByName(auth.SQSClient, ctx.QueueName)
	)

	log.Infof("starting lifecycle-manager service v%v", version.Version)
	log.Infof("region = %v", ctx.Region)
	log.Infof("queue = %v", url)
	log.Infof("polling interval seconds = %v", ctx.PollingIntervalSeconds)
	log.Infof("drain timeout seconds = %v", ctx.DrainTimeoutSeconds)
	log.Infof("drain retry interval seconds = %v", ctx.DrainRetryIntervalSeconds)

	// create a poller goroutine that reads from sqs and posts to channel
	log.Info("spawning sqs poller")
	go newQueuePoller(auth.SQSClient, stream, url, ctx.PollingIntervalSeconds)

	// process messags from channel
	for message := range g.eventStream {
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
		go g.Process(event)
	}

}

// Process processes a received event
func (g *Manager) Process(event *LifecycleEvent) {
	var (
		queue              = g.authenticator.SQSClient
		scalingGroupClient = g.authenticator.ScalingGroupClient
		url                = event.queueURL
	)

	if event.IsAlreadyExist(g.queue) {
		err := deleteMessage(queue, url, event.receiptHandle)
		if err != nil {
			log.Errorf("failed to delete message: %v", err)
		}
		return
	}
	g.AddEvent(*event)

	err := g.handleEvent(event)
	if err != nil {
		log.Errorf("failed to process event: %v", err)
		log.Warnf("abandoning instance '%v', ", event.EC2InstanceID)
		completeLifecycleAction(scalingGroupClient, *event, AbandonAction)
	}

	err = deleteMessage(queue, url, event.receiptHandle)
	if err != nil {
		log.Errorf("failed to delete message: %v", err)
	}
	g.CompleteEvent(*event)
}

func (g *Manager) handleEvent(event *LifecycleEvent) error {
	var (
		kubeClient  = g.authenticator.KubernetesClient
		asgClient   = g.authenticator.ScalingGroupClient
		ssmClient   = g.authenticator.SSMClient
		ctx         = &g.context
		kubectlPath = ctx.KubectlLocalPath
	)
	// validate instance-id exists as a node
	node, exists := getNodeByInstance(kubeClient, event.EC2InstanceID)
	if !exists {
		return errors.Errorf("instance '%v' is not seen in cluster nodes", event.EC2InstanceID)
	}
	event.SetReferencedNode(node)

	// send heartbeat at intervals
	recommendedHeartbeatActionInterval := event.heartbeatInterval / 2
	log.Infof("hook heartbeat timeout interval is %v, will send heartbeat every %v seconds", event.heartbeatInterval, recommendedHeartbeatActionInterval)
	go sendHeartbeat(asgClient, event, recommendedHeartbeatActionInterval)

	// drain action
	err := drainNode(kubectlPath, event.referencedNode.Name, ctx.DrainTimeoutSeconds, ctx.DrainRetryIntervalSeconds)
	if err != nil {
		return err
	}
	log.Infof("completed drain for node '%v' -> %v", event.referencedNode.Name, event.EC2InstanceID)
	event.SetDrainCompleted(true)

	// run-command
	if ctx.RunCommand {
		log.Infof("running SSM document: %v on %v -> %v", ctx.CommandDocument, event.referencedNode.Name, event.EC2InstanceID)
		commandID, err := sendCommand(ssmClient, ctx.CommandDocument, event.EC2InstanceID)
		if err != nil {
			return err
		}
		log.Infof("waiting for command execution: %v on %v -> %v", commandID, event.referencedNode.Name, event.EC2InstanceID)
		err = waitForCommand(ssmClient, commandID, event.EC2InstanceID)
		if err != nil {
			return err
		}
		event.SetCommandSucceeded(true)

		if ctx.DeleteNode {
			log.Infof("deleting node object '%v'", event.referencedNode.Name)
			err = deleteNode(kubeClient, event.referencedNode.Name)
			if err != nil {
				return err
			}
			event.SetNodeDeleted(true)
		}
		log.Infof("completed command execution for node '%v' -> %v", event.referencedNode.Name, event.EC2InstanceID)
	}

	// complete lifecycle action
	err = completeLifecycleAction(asgClient, *event, ContinueAction)
	if err != nil {
		return err
	}

	return nil
}
