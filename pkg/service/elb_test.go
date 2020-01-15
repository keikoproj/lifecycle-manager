package service

import (
	"fmt"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
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

func (e *stubErrorELB) DescribeInstanceHealth(input *elb.DescribeInstanceHealthInput) (*elb.DescribeInstanceHealthOutput, error) {
	e.timesCalledDescribeInstanceHealth++
	var err error
	if e.failHint == elb.ErrCodeAccessPointNotFoundException {
		err = awserr.New(elb.ErrCodeAccessPointNotFoundException, "failed", fmt.Errorf("it failed"))
	} else {
		err = fmt.Errorf("some error, DeregisterTargets")
	}
	return &elb.DescribeInstanceHealthOutput{}, err
}

func (e *stubErrorELB) DeregisterInstancesFromLoadBalancer(input *elb.DeregisterInstancesFromLoadBalancerInput) (*elb.DeregisterInstancesFromLoadBalancerOutput, error) {
	e.timesCalledDeregisterInstances++
	var err error
	if e.failHint == elb.ErrCodeAccessPointNotFoundException {
		err = awserr.New(elb.ErrCodeAccessPointNotFoundException, "failed", fmt.Errorf("it failed"))
	} else if e.failHint == elb.ErrCodeInvalidEndPointException {
		err = awserr.New(elb.ErrCodeInvalidEndPointException, "failed", fmt.Errorf("it failed"))
	} else {
		err = fmt.Errorf("some other error occured")
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

func Test_DeregisterNotFoundException(t *testing.T) {
	t.Log("Test_DeregisterError: should return an error when call fails")
	var (
		stubber       = &stubErrorELB{}
		elbName       = "some-load-balancer"
		instanceID    = "i-1234567890"
		expectedCalls = 1
	)

	stubber.failHint = elb.ErrCodeAccessPointNotFoundException
	err := deregisterInstance(stubber, elbName, instanceID)
	if err == nil {
		t.Fatalf("Test_DeregisterInstance: expected error to have occured, got: %v", err)
	}

	if stubber.timesCalledDeregisterInstances != expectedCalls {
		t.Fatalf("Test_DeregisterInstance: expected timesCalledDeregisterInstances: %v, got: %v", expectedCalls, stubber.timesCalledDeregisterInstances)
	}
}

func Test_DeregisterInvalidException(t *testing.T) {
	t.Log("Test_DeregisterError: should return an error when call fails")
	var (
		stubber       = &stubErrorELB{}
		elbName       = "some-load-balancer"
		instanceID    = "i-1234567890"
		expectedCalls = 1
	)

	stubber.failHint = elb.ErrCodeInvalidEndPointException
	err := deregisterInstance(stubber, elbName, instanceID)
	if err == nil {
		t.Fatalf("Test_DeregisterInstance: expected error to have occured, got: %v", err)
	}

	if stubber.timesCalledDeregisterInstances != expectedCalls {
		t.Fatalf("Test_DeregisterInstance: expected timesCalledDeregisterInstances: %v, got: %v", expectedCalls, stubber.timesCalledDeregisterInstances)
	}
}

func Test_DeregisterWaiterAbort(t *testing.T) {
	t.Log("Test_DeregisterWaiterAbort: should stop waiter when event is completed")
	var (
		event         = &LifecycleEvent{}
		elbName       = "some-load-balancer"
		instanceID    = "i-1234567890"
		expectedCalls = 2
	)

	stubber := &stubELB{
		instanceStates: []*elb.InstanceState{
			{
				InstanceId: aws.String(instanceID),
				State:      aws.String("InService"),
			},
		},
	}

	go _completeEventAfter(event, time.Second*1)
	err := waitForDeregisterInstance(event, stubber, elbName, instanceID)
	if err == nil {
		t.Fatalf("Test_DeregisterWaiterAbort: expected error to have occured, got: %v", err)
	}

	if stubber.timesCalledDescribeInstanceHealth != expectedCalls {
		t.Fatalf("Test_DeregisterWaiterAbort: expected timesCalledDescribeInstanceHealth: %v, got: %v", expectedCalls, stubber.timesCalledDescribeInstanceHealth)
	}
}

func Test_DeregisterWaiterFail(t *testing.T) {
	t.Log("Test_DeregisterWaiterFail: should return error when call fails")
	var (
		event         = &LifecycleEvent{}
		elbName       = "some-load-balancer"
		instanceID    = "i-1234567890"
		expectedCalls = 1
	)

	stubber := &stubErrorELB{
		failHint: elb.ErrCodeAccessPointNotFoundException,
	}

	err := waitForDeregisterInstance(event, stubber, elbName, instanceID)
	if err == nil {
		t.Fatalf("Test_DeregisterWaiterFail: expected error to have occured, got: %v", err)
	}

	if stubber.timesCalledDescribeInstanceHealth != expectedCalls {
		t.Fatalf("Test_DeregisterWaiterFail: expected timesCalledDescribeInstanceHealth: %v, got: %v", expectedCalls, stubber.timesCalledDescribeInstanceHealth)
	}
}

func Test_DeregisterWaiterNotFound(t *testing.T) {
	t.Log("Test_DeregisterWaiterNotFound: should return without error instance not found")
	var (
		event         = &LifecycleEvent{}
		elbName       = "some-load-balancer"
		instanceID    = "i-1234567890"
		expectedCalls = 1
	)

	stubber := &stubELB{
		instanceStates: []*elb.InstanceState{
			{
				InstanceId: aws.String("some-other-instance"),
				State:      aws.String("InService"),
			},
		},
	}

	err := waitForDeregisterInstance(event, stubber, elbName, instanceID)
	if err != nil {
		t.Fatalf("Test_DeregisterWaiterNotFound: expected error not to have occured, got: %v", err)
	}

	if stubber.timesCalledDescribeInstanceHealth != expectedCalls {
		t.Fatalf("Test_DeregisterWaiterNotFound: expected timesCalledDescribeInstanceHealth: %v, got: %v", expectedCalls, stubber.timesCalledDescribeInstanceHealth)
	}
}

func Test_DeregisterWaiterTimeout(t *testing.T) {
	t.Log("Test_DeregisterWaiterTimeout: should return error when waiter times out")
	var (
		event         = &LifecycleEvent{}
		elbName       = "some-load-balancer"
		instanceID    = "i-1234567890"
		expectedCalls = 3
	)

	stubber := &stubELB{
		instanceStates: []*elb.InstanceState{
			{
				InstanceId: aws.String(instanceID),
				State:      aws.String("InService"),
			},
		},
	}

	err := waitForDeregisterInstance(event, stubber, elbName, instanceID)
	if err == nil {
		t.Fatalf("Test_DeregisterWaiterTimeout: expected error to have occured, got: %v", err)
	}

	if stubber.timesCalledDescribeInstanceHealth != expectedCalls {
		t.Fatalf("Test_DeregisterWaiterTimeout: expected timesCalledDescribeInstanceHealth: %v, got: %v", expectedCalls, stubber.timesCalledDescribeInstanceHealth)
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

func Test_FindInstanceInClassicBalancerError(t *testing.T) {
	t.Log("Test_FindInstanceInClassicBalancerError: should return error if call fails")
	var (
		elbName       = "some-load-balancer"
		instanceID    = "i-1234567890"
		expectedCalls = 1
		stubber       = &stubErrorELB{}
	)

	stubber.failHint = elb.ErrCodeAccessPointNotFoundException
	found, err := findInstanceInClassicBalancer(stubber, elbName, instanceID)
	if err == nil {
		t.Fatalf("Test_FindInstanceInClassicBalancerError: expected error to have occured, got: %v", err)
	}
	if stubber.timesCalledDescribeInstanceHealth != expectedCalls {
		t.Fatalf("expected Test_FindInstanceInClassicBalancerError: %v, got: %v", expectedCalls, stubber.timesCalledDescribeInstanceHealth)
	}
	if found {
		t.Fatalf("Test_FindInstanceInClassicBalancerError: expected instance not to be found")
	}
}
