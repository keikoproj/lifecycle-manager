package service

import (
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/autoscaling"
	"github.com/aws/aws-sdk-go/service/autoscaling/autoscalingiface"
	"github.com/keikoproj/lifecycle-manager/pkg/log"
)

func sendHeartbeat(client autoscalingiface.AutoScalingAPI, event *LifecycleEvent, maxTimeToProcessSeconds int64) {
	var (
		iterationCount      = 0
		interval            = event.heartbeatInterval
		instanceID          = event.EC2InstanceID
		scalingGroupName    = event.AutoScalingGroupName
		recommendedInterval = interval / 2
	)

	log.Debugf("scaling-group = %v, maxInterval = %v, heartbeat = %v", scalingGroupName, interval, recommendedInterval)

	// max time to process an event is capped at 1hr
	maxIterations := int(maxTimeToProcessSeconds / recommendedInterval)

	for {
		iterationCount++
		if iterationCount >= maxIterations {
			// hard limit in case event is not marked completed
			log.Warnf("%v> heartbeat extended over threshold, instance will be abandoned", instanceID)
			event.SetEventCompleted(true)
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
