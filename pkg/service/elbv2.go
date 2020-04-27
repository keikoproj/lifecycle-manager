package service

import (
	"errors"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/elbv2"
	"github.com/aws/aws-sdk-go/service/elbv2/elbv2iface"
	iebackoff "github.com/keikoproj/inverse-exp-backoff"

	"github.com/keikoproj/lifecycle-manager/pkg/log"
)

func waitForDeregisterTarget(event *LifecycleEvent, elbClient elbv2iface.ELBV2API, arn, instanceID string, port int64) error {
	var (
		found bool
	)

	input := &elbv2.DescribeTargetHealthInput{
		TargetGroupArn: aws.String(arn),
	}

	for ieb, err := iebackoff.NewIEBackoff(WaiterMaxDelay, WaiterMinDelay, 0.5, WaiterMaxAttempts); err == nil; err = ieb.Next() {

		if event.eventCompleted {
			return errors.New("event finished execution during deregistration wait")
		}

		found = false
		targets, err := elbClient.DescribeTargetHealth(input)
		if err != nil {
			return err
		}
		for _, targetDescription := range targets.TargetHealthDescriptions {
			if aws.StringValue(targetDescription.Target.Id) == instanceID && aws.Int64Value(targetDescription.Target.Port) == port {
				found = true
				if aws.StringValue(targetDescription.TargetHealth.State) == elbv2.TargetHealthStateEnumUnused {
					return nil
				}
				break
			}
		}
		if !found {
			log.Debugf("%v> target not found in target group %v", instanceID, arn)
			return nil
		}
		log.Debugf("%v> deregistration from %v pending", instanceID, arn)
	}

	err := errors.New("wait for target deregister timed out")
	return err
}

func findInstanceInTargetGroup(elbClient elbv2iface.ELBV2API, arn, instanceID string) (bool, int64, error) {
	input := &elbv2.DescribeTargetHealthInput{
		TargetGroupArn: aws.String(arn),
	}

	target, err := elbClient.DescribeTargetHealth(input)
	if err != nil {
		log.Errorf("%v> failed finding instance in target group %v: %v", instanceID, arn, err.Error())
		return false, 0, err
	}
	for _, desc := range target.TargetHealthDescriptions {
		if aws.StringValue(desc.Target.Id) == instanceID {
			port := aws.Int64Value(desc.Target.Port)
			return true, port, nil
		}
	}
	return false, 0, nil
}

func deregisterTargets(elbClient elbv2iface.ELBV2API, arn string, mapping map[string]int64) error {

	targets := []*elbv2.TargetDescription{}
	for instance, port := range mapping {
		target := &elbv2.TargetDescription{
			Id:   aws.String(instance),
			Port: aws.Int64(port),
		}
		targets = append(targets, target)
	}

	input := &elbv2.DeregisterTargetsInput{
		Targets:        targets,
		TargetGroupArn: aws.String(arn),
	}

	_, err := elbClient.DeregisterTargets(input)
	if err != nil {
		return err
	}
	return nil
}
