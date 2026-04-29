package service

import (
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/autoscaling"
	"github.com/aws/aws-sdk-go/service/autoscaling/autoscalingiface"
	"github.com/keikoproj/lifecycle-manager/pkg/log"
)

type heartbeatConfig struct {
	asgClient               autoscalingiface.AutoScalingAPI
	event                   *LifecycleEvent
	maxTimeToProcessSeconds int64
}

// sendHeartbeat keeps the AWS lifecycle hook alive by periodically calling
// RecordLifecycleActionHeartbeat. It exits when the main goroutine sets
// event.eventCompleted (via CompleteEvent or FailEvent), when extendLifecycleAction
// fails (e.g., the hook has been completed/abandoned by another path), or when
// the safety cap maxTimeToProcessSeconds is reached.
func sendHeartbeat(cfg heartbeatConfig) {
	var (
		event               = cfg.event
		client              = cfg.asgClient
		interval            = event.heartbeatInterval
		instanceID          = event.EC2InstanceID
		scalingGroupName    = event.AutoScalingGroupName
		recommendedInterval = interval / 2
	)

	log.Debugf("scaling-group = %v, maxInterval = %v, heartbeat = %v", scalingGroupName, interval, recommendedInterval)

	startTime := time.Now()
	deadline := startTime.Add(time.Duration(cfg.maxTimeToProcessSeconds) * time.Second)

	iterationCount := 0
	for {
		if event.eventCompleted {
			return
		}
		if time.Now().After(deadline) {
			elapsed := time.Since(startTime).Round(time.Second)
			log.Warnf("%v> heartbeat reached max-time-to-process safety cap after %v; stopping heartbeats (main goroutine appears stuck)", instanceID, elapsed)
			return
		}

		iterationCount++
		log.Infof("%v> sending heartbeat (iteration %v)", instanceID, iterationCount)
		if err := extendLifecycleAction(client, *event); err != nil {
			log.Errorf("%v> failed to send heartbeat for event: %v", instanceID, err)
			return
		}

		time.Sleep(time.Duration(recommendedInterval) * time.Second)
	}
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
