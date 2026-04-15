package service

import (
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/autoscaling"
	"github.com/aws/aws-sdk-go/service/autoscaling/autoscalingiface"
	iebackoff "github.com/keikoproj/inverse-exp-backoff"
	"github.com/keikoproj/lifecycle-manager/pkg/log"
	"k8s.io/client-go/kubernetes"
)

type heartbeatConfig struct {
	asgClient                 autoscalingiface.AutoScalingAPI
	kubeClient                kubernetes.Interface
	event                     *LifecycleEvent
	maxTimeToProcessSeconds   int64
	maxTerminationGracePeriod int64
}

func sendHeartbeat(cfg heartbeatConfig) {
	var (
		event               = cfg.event
		client              = cfg.asgClient
		iterationCount      = 0
		interval            = event.heartbeatInterval
		instanceID          = event.EC2InstanceID
		scalingGroupName    = event.AutoScalingGroupName
		recommendedInterval = interval / 2
	)

	log.Debugf("scaling-group = %v, maxInterval = %v, heartbeat = %v", scalingGroupName, interval, recommendedInterval)

	maxIterations := int(cfg.maxTimeToProcessSeconds / recommendedInterval)

	startTime := time.Now()

	for {
		iterationCount++
		if iterationCount >= maxIterations {
			elapsed := time.Since(startTime).Round(time.Second)
			log.Warnf("%v> heartbeat extended over threshold after %v, beginning graceful pod termination", instanceID, elapsed)
			handleHeartbeatTimeout(cfg)
			return
		}

		if event.eventCompleted {
			return
		}

		log.Infof("%v> sending heartbeat (%v/%v)", instanceID, iterationCount, maxIterations)
		err := extendLifecycleAction(client, *event)
		if err != nil {
			log.Errorf("%v> failed to send heartbeat for event: %v", instanceID, err)
			return
		}
		time.Sleep(time.Duration(recommendedInterval) * time.Second)
	}
}

func handleHeartbeatTimeout(cfg heartbeatConfig) {
	var (
		event      = cfg.event
		client     = cfg.asgClient
		kubeClient = cfg.kubeClient
		instanceID = event.EC2InstanceID
		nodeName   = event.referencedNode.Name
	)

	deleted, err := deletePodsOnNode(kubeClient, nodeName, cfg.maxTerminationGracePeriod)
	if err != nil {
		log.Errorf("%v> failed to delete pods on node %s: %v", instanceID, nodeName, err)
	} else {
		log.Infof("%v> requested deletion of %d non-daemonset pods on node %s", instanceID, deleted, nodeName)
	}

	msg := fmt.Sprintf(EventMessageHeartbeatTimeoutGracefulTermination, instanceID, deleted, nodeName)
	kEvent := newKubernetesEvent(EventReasonHeartbeatTimeoutGracefulTermination, getMessageFields(event, msg))
	publishKubernetesEvent(kubeClient, kEvent)

	waitForPodsWithHeartbeat(cfg)

	if event.eventCompleted {
		log.Infof("%v> event already completed by main goroutine, skipping ABANDON", instanceID)
	} else {
		log.Warnf("%v> completing lifecycle action with ABANDON after timeout", instanceID)
		if err := completeLifecycleAction(client, *event, AbandonAction); err != nil {
			log.Errorf("%v> failed to complete lifecycle action with ABANDON: %v", instanceID, err)
		}
	}

	event.SetEventCompleted(true)
}

func waitForPodsWithHeartbeat(cfg heartbeatConfig) {
	var (
		event      = cfg.event
		client     = cfg.asgClient
		kubeClient = cfg.kubeClient
		instanceID = event.EC2InstanceID
		nodeName   = event.referencedNode.Name
		timeout    = time.Duration(cfg.maxTerminationGracePeriod) * time.Second
		maxDelay   = WaiterMaxDelay
		minDelay   = WaiterMinDelay
	)

	if timeout <= 0 {
		return
	}
	if maxDelay > timeout {
		maxDelay = timeout / 2
	}
	if minDelay > maxDelay {
		minDelay = maxDelay
	}

	ieb, err := iebackoff.NewIEBWithTimeout(maxDelay, minDelay, timeout, 0.5, time.Now())
	if err != nil {
		log.Errorf("%v> failed to create inverse-exp-backoff: %v", instanceID, err)
		return
	}

	for ; err == nil; err = ieb.Next() {
		if err := extendLifecycleAction(client, *event); err != nil {
			log.Errorf("%v> failed to send heartbeat during termination grace period: %v", instanceID, err)
		}

		active := countActivePods(kubeClient, nodeName)
		if active == 0 {
			log.Infof("%v> all non-daemonset pods terminated on node %s", instanceID, nodeName)
			return
		}
		if active > 0 {
			log.Infof("%v> waiting for %d non-daemonset pods to terminate on node %s", instanceID, active, nodeName)
		}
	}

	log.Warnf("%v> termination grace period expired with pods still running on node %s", instanceID, nodeName)
}

func getHookHeartbeatInterval(client autoscalingiface.AutoScalingAPI, lifecycleHookName, scalingGroupName string) (int64, error) {
	input := &autoscaling.DescribeLifecycleHooksInput{
		AutoScalingGroupName: aws.String(scalingGroupName),
		LifecycleHookNames:   aws.StringSlice([]string{lifecycleHookName}),
	}
	out, err := client.DescribeLifecycleHooks(input)
	if err != nil {
		return 0, err
	}

	if len(out.LifecycleHooks) == 0 {
		err = fmt.Errorf("could not find lifecycle hook with name %v for scaling group %v", lifecycleHookName, scalingGroupName)
		return 0, err
	}

	return aws.Int64Value(out.LifecycleHooks[0].HeartbeatTimeout), nil
}

func completeLifecycleAction(client autoscalingiface.AutoScalingAPI, event LifecycleEvent, result string) error {
	log.Infof("%v> setting lifecycle event as completed with result: %v", event.EC2InstanceID, result)
	input := &autoscaling.CompleteLifecycleActionInput{
		AutoScalingGroupName:  aws.String(event.AutoScalingGroupName),
		InstanceId:            aws.String(event.EC2InstanceID),
		LifecycleActionResult: aws.String(result),
		LifecycleHookName:     aws.String(event.LifecycleHookName),
	}
	_, err := client.CompleteLifecycleAction(input)
	if err != nil {
		return err
	}
	return nil
}

func extendLifecycleAction(client autoscalingiface.AutoScalingAPI, event LifecycleEvent) error {
	log.Debugf("%v> extending lifecycle event", event.EC2InstanceID)
	input := &autoscaling.RecordLifecycleActionHeartbeatInput{
		AutoScalingGroupName: aws.String(event.AutoScalingGroupName),
		InstanceId:           aws.String(event.EC2InstanceID),
		LifecycleActionToken: aws.String(event.LifecycleActionToken),
		LifecycleHookName:    aws.String(event.LifecycleHookName),
	}
	_, err := client.RecordLifecycleActionHeartbeat(input)
	if err != nil {
		return err
	}
	return nil
}
