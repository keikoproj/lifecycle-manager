package service

import (
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/autoscaling"
	"github.com/aws/aws-sdk-go/service/autoscaling/autoscalingiface"
	"github.com/keikoproj/lifecycle-manager/pkg/log"
)

func sendHeartbeat(client autoscalingiface.AutoScalingAPI, event *LifecycleEvent, sleepInterval int64) {
	for {
		time.Sleep(time.Duration(sleepInterval) * time.Second)
		if event.drainCompleted {
			return
		}
		log.Infof("sending heartbeat for event with instance '%v' and sleeping for %v seconds", event.EC2InstanceID, sleepInterval)
		err := extendLifecycleAction(client, *event)
		if err != nil {
			log.Errorf("failed to send heartbeat for event with instance '%v': %v", event.EC2InstanceID, err)
			return
		}
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
	log.Infof("setting lifecycle event as completed with result: '%v'", result)
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
	log.Debugf("extending lifecycle event for %v", event.EC2InstanceID)
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
