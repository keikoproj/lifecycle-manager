package service

import (
	"context"
	"fmt"
	"testing"

	"github.com/aws/aws-sdk-go/aws/request"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/elb"
	"github.com/aws/aws-sdk-go/service/elb/elbiface"
)

type stubELB struct {
	elbiface.ELBAPI
	instanceStates                    []*elb.InstanceState
	loadBalancerDescriptions          []*elb.LoadBalancerDescription
	timesCalledDescribeInstanceHealth int
	timesCalledDeregisterInstances    int
	timesCalledDescribeLoadBalancers  int
}

func (e *stubELB) DescribeInstanceHealth(input *elb.DescribeInstanceHealthInput) (*elb.DescribeInstanceHealthOutput, error) {
	e.timesCalledDescribeInstanceHealth++
	return &elb.DescribeInstanceHealthOutput{InstanceStates: e.instanceStates}, nil
}

func (e *stubELB) DeregisterInstancesFromLoadBalancer(input *elb.DeregisterInstancesFromLoadBalancerInput) (*elb.DeregisterInstancesFromLoadBalancerOutput, error) {
	e.timesCalledDeregisterInstances++
	return &elb.DeregisterInstancesFromLoadBalancerOutput{}, nil
}

func (e *stubELB) DescribeLoadBalancers(input *elb.DescribeLoadBalancersInput) (*elb.DescribeLoadBalancersOutput, error) {
	e.timesCalledDescribeLoadBalancers++
	return &elb.DescribeLoadBalancersOutput{LoadBalancerDescriptions: e.loadBalancerDescriptions}, nil
}

func (e *stubELB) DescribeLoadBalancersPages(input *elb.DescribeLoadBalancersInput, callback func(*elb.DescribeLoadBalancersOutput, bool) bool) error {
	page, err := e.DescribeLoadBalancers(input)
	if err != nil {
		return err
	}

	callback(page, false)

	return nil
}

type stubErrorELB struct {
	elbiface.ELBAPI
	instanceStates                    []*elb.InstanceState
	loadBalancerDescriptions          []*elb.LoadBalancerDescription
	timesCalledDescribeInstanceHealth int
	timesCalledDeregisterInstances    int
	timesCalledDescribeLoadBalancers  int
	failHint                          string
}

func (e *stubErrorELB) WaitUntilInstanceDeregisteredWithContext(ctx context.Context, input *elb.DescribeInstanceHealthInput, req ...request.WaiterOption) error {
	var err error
	if e.failHint == "WaitUntilInstanceDeregisteredWithContext" {
		err = fmt.Errorf("inject error, DeregisterTargets")
	}
	return err
}

func (e *stubErrorELB) DescribeInstanceHealth(input *elb.DescribeInstanceHealthInput) (*elb.DescribeInstanceHealthOutput, error) {
	e.timesCalledDescribeInstanceHealth++
	return &elb.DescribeInstanceHealthOutput{InstanceStates: e.instanceStates}, nil
}

func (e *stubErrorELB) DeregisterInstancesFromLoadBalancer(input *elb.DeregisterInstancesFromLoadBalancerInput) (*elb.DeregisterInstancesFromLoadBalancerOutput, error) {
	e.timesCalledDeregisterInstances++
	var err error
	if e.failHint == "DeregisterInstancesFromLoadBalancer" {
		err = fmt.Errorf("inject error, DeregisterTargets")
	}
	return &elb.DeregisterInstancesFromLoadBalancerOutput{}, err
}

func (e *stubErrorELB) DescribeLoadBalancers(input *elb.DescribeLoadBalancersInput) (*elb.DescribeLoadBalancersOutput, error) {
	e.timesCalledDescribeLoadBalancers++
	return &elb.DescribeLoadBalancersOutput{LoadBalancerDescriptions: e.loadBalancerDescriptions}, nil
}

func (e *stubErrorELB) DescribeLoadBalancersPages(input *elb.DescribeLoadBalancersInput, callback func(*elb.DescribeLoadBalancersOutput, bool) bool) error {
	page, err := e.DescribeLoadBalancers(input)
	if err != nil {
		return err
	}

	callback(page, false)

	return nil
}

func Test_DeregisterInstance(t *testing.T) {
	t.Log("Test_DeregisterInstance: should be able to deregister an instance from classic elb")
	var (
		stubber       = &stubELB{}
		elbName       = "some-load-balancer"
		instanceID    = "i-1234567890"
		expectedCalls = 1
	)

	err := deregisterInstance(stubber, elbName, instanceID)
	if err != nil {
		t.Fatalf("Test_DeregisterInstance: expected error not to have occured, %v", err)
	}

	if stubber.timesCalledDeregisterInstances != expectedCalls {
		t.Fatalf("Test_DeregisterInstance: expected timesCalledDeregisterInstances: %v, got: %v", expectedCalls, stubber.timesCalledDeregisterInstances)
	}
}

func Test_FindInstanceInClassicBalancerPositive(t *testing.T) {
	t.Log("Test_FindInstanceInTargetGroupPositive: should be able to find instance in target group if it exists")
	var (
		elbName       = "some-load-balancer"
		instanceID    = "i-1234567890"
		expectedCalls = 1
	)
	stubber := &stubELB{
		instanceStates: []*elb.InstanceState{
			{
				InstanceId: aws.String(instanceID),
			},
		},
	}

	found, err := findInstanceInClassicBalancer(stubber, elbName, instanceID)
	if err != nil {
		t.Fatalf("Test_FindInstanceInClassicBalancerPositive: expected error not to have occured, %v", err)
	}
	if stubber.timesCalledDescribeInstanceHealth != expectedCalls {
		t.Fatalf("expected timesCalledDescribeInstanceHealth: %v, got: %v", expectedCalls, stubber.timesCalledDescribeInstanceHealth)
	}
	if !found {
		t.Fatalf("Test_FindInstanceInClassicBalancerPositive: expected instance to be found")
	}
}

func Test_FindInstanceInClassicBalancerNegative(t *testing.T) {
	t.Log("Test_FindInstanceInClassicBalancerNegative: should be able to find instance in target group if it exists")
	var (
		elbName       = "some-load-balancer"
		instanceID    = "i-1234567890"
		expectedCalls = 1
		stubber       = &stubELB{}
	)

	found, err := findInstanceInClassicBalancer(stubber, elbName, instanceID)
	if err != nil {
		t.Fatalf("Test_FindInstanceInClassicBalancerNegative: expected error not to have occured, %v", err)
	}
	if stubber.timesCalledDescribeInstanceHealth != expectedCalls {
		t.Fatalf("expected timesCalledDescribeInstanceHealth: %v, got: %v", expectedCalls, stubber.timesCalledDescribeInstanceHealth)
	}
	if found {
		t.Fatalf("Test_FindInstanceInClassicBalancerNegative: expected instance not to be found")
	}
}
