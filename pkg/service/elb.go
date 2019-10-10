package service

import (
	"context"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/request"
	"github.com/aws/aws-sdk-go/service/elb"
	"github.com/aws/aws-sdk-go/service/elb/elbiface"

	"github.com/keikoproj/lifecycle-manager/pkg/log"
)

func waitForDeregisterInstance(elbClient elbiface.ELBAPI, elbName, instanceID string) error {
	var (
		MaxAttempts = 500
		ConstDelay  = request.ConstantWaiterDelay(10 * time.Second)
	)

	waiterOpts := []request.WaiterOption{
		request.WithWaiterMaxAttempts(MaxAttempts),
		request.WithWaiterDelay(ConstDelay),
	}
	input := &elb.DescribeInstanceHealthInput{
		LoadBalancerName: aws.String(elbName),
		Instances: []*elb.Instance{
			{
				InstanceId: aws.String(instanceID),
			},
		},
	}

	err := elbClient.WaitUntilInstanceDeregisteredWithContext(context.Background(), input, waiterOpts...)
	if err != nil {
		return err
	}
	return nil
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
