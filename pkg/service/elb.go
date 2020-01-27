package service

import (
	"errors"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/elb"
	"github.com/aws/aws-sdk-go/service/elb/elbiface"
	iebackoff "github.com/keikoproj/inverse-exp-backoff"

	"github.com/keikoproj/lifecycle-manager/pkg/log"
)

func waitForDeregisterInstance(event *LifecycleEvent, elbClient elbiface.ELBAPI, elbName, instanceID string) error {
	var (
		found bool
	)

	input := &elb.DescribeInstanceHealthInput{
		LoadBalancerName: aws.String(elbName),
	}

	for ieb, err := iebackoff.NewIEBackoff(WaiterMinDelay, WaiterDelayInterval, 0.5, WaiterMaxAttempts); err == nil; err = ieb.Next() {

		if event.eventCompleted {
			return errors.New("event finished execution during deregistration wait")
		}

		found = false
		instances, err := elbClient.DescribeInstanceHealth(input)
		if err != nil {
			return err
		}
		for _, state := range instances.InstanceStates {
			if aws.StringValue(state.InstanceId) == instanceID {
				found = true
				if aws.StringValue(state.State) == "OutOfService" {
					return nil
				}
				break
			}
		}
		if !found {
			log.Debugf("%v> instance not found in elb %v", instanceID, elbName)
			return nil
		}
		log.Debugf("%v> deregistration from %v pending", instanceID, elbName)
	}

	err := errors.New("wait for target deregister timed out")
	return err
}

func findInstanceInClassicBalancer(elbClient elbiface.ELBAPI, elbName, instanceID string) (bool, error) {
	input := &elb.DescribeInstanceHealthInput{
		LoadBalancerName: aws.String(elbName),
	}

	instance, err := elbClient.DescribeInstanceHealth(input)
	if err != nil {
		log.Errorf("%v> failed finding instance in elb %v: %v", instanceID, elbName, err.Error())
		return false, err
	}
	for _, state := range instance.InstanceStates {
		if aws.StringValue(state.InstanceId) == instanceID {
			return true, nil
		}
	}
	return false, nil
}

func deregisterInstances(elbClient elbiface.ELBAPI, elbName string, instances []string) error {
	targets := []*elb.Instance{}
	for _, instance := range instances {
		target := &elb.Instance{
			InstanceId: aws.String(instance),
		}
		targets = append(targets, target)
	}

	input := &elb.DeregisterInstancesFromLoadBalancerInput{
		LoadBalancerName: aws.String(elbName),
		Instances:        targets,
	}

	_, err := elbClient.DeregisterInstancesFromLoadBalancer(input)
	if err != nil {
		return err
	}
	return nil
}
