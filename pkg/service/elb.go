package service

import (
	"errors"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/elb"
	"github.com/aws/aws-sdk-go/service/elb/elbiface"

	"github.com/keikoproj/lifecycle-manager/pkg/log"
)

func waitForDeregisterInstance(event *LifecycleEvent, elbClient elbiface.ELBAPI, elbName, instanceID string) error {
	var (
		DelayIntervalSeconds int64 = 30
		MaxAttempts                = 500
		found                bool
	)

	input := &elb.DescribeInstanceHealthInput{
		LoadBalancerName: aws.String(elbName),
	}

	for i := 0; i < MaxAttempts; i++ {

		if event.eventCompleted {
			return errors.New("event completed before waiter completed")
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
			log.Infof("instance %v not found in elb %v", instanceID, elbName)
			return nil
		}
		log.Infof("instance %v not yet deregistered from load balancer %v, waiting %vs", instanceID, elbName, DelayIntervalSeconds)
		time.Sleep(time.Second * time.Duration(DelayIntervalSeconds))
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
		return false, err
	}
	for _, state := range instance.InstanceStates {
		if aws.StringValue(state.InstanceId) == instanceID {
			return true, nil
		}
	}
	return false, nil
}

func deregisterInstance(elbClient elbiface.ELBAPI, elbName, instanceID string) error {
	input := &elb.DeregisterInstancesFromLoadBalancerInput{
		LoadBalancerName: aws.String(elbName),
		Instances: []*elb.Instance{
			{
				InstanceId: aws.String(instanceID),
			},
		},
	}

	log.Infof("deregistering %v from %v", instanceID, elbName)
	_, err := elbClient.DeregisterInstancesFromLoadBalancer(input)
	if err != nil {
		return err
	}
	return nil
}
