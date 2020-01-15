package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/request"
	"github.com/aws/aws-sdk-go/service/elbv2"
	"github.com/aws/aws-sdk-go/service/elbv2/elbv2iface"
)

type stubELBv2 struct {
	elbv2iface.ELBV2API
	targetHealthDescriptions        []*elbv2.TargetHealthDescription
	targetGroups                    []*elbv2.TargetGroup
	timesCalledDescribeTargetHealth int
	timesCalledDeregisterTargets    int
	timesCalledDescribeTargetGroups int
}

func (e *stubELBv2) WaitUntilTargetDeregisteredWithContext(ctx context.Context, input *elbv2.DescribeTargetHealthInput, req ...request.WaiterOption) error {
	return nil
}

func (e *stubELBv2) DescribeTargetHealth(input *elbv2.DescribeTargetHealthInput) (*elbv2.DescribeTargetHealthOutput, error) {
	e.timesCalledDescribeTargetHealth++
	return &elbv2.DescribeTargetHealthOutput{TargetHealthDescriptions: e.targetHealthDescriptions}, nil
}

func (e *stubELBv2) DeregisterTargets(input *elbv2.DeregisterTargetsInput) (*elbv2.DeregisterTargetsOutput, error) {
	e.timesCalledDeregisterTargets++
	return &elbv2.DeregisterTargetsOutput{}, nil
}

func (e *stubELBv2) DescribeTargetGroups(input *elbv2.DescribeTargetGroupsInput) (*elbv2.DescribeTargetGroupsOutput, error) {
	e.timesCalledDescribeTargetGroups++
	return &elbv2.DescribeTargetGroupsOutput{TargetGroups: e.targetGroups}, nil
}

func (e *stubELBv2) DescribeTargetGroupsPages(input *elbv2.DescribeTargetGroupsInput, callback func(*elbv2.DescribeTargetGroupsOutput, bool) bool) error {
	page, err := e.DescribeTargetGroups(input)
	if err != nil {
		return err
	}

	callback(page, false)

	return nil
}

type stubErrorELBv2 struct {
	elbv2iface.ELBV2API
	targetHealthDescriptions        []*elbv2.TargetHealthDescription
	targetGroups                    []*elbv2.TargetGroup
	timesCalledDescribeTargetHealth int
	timesCalledDeregisterTargets    int
	timesCalledDescribeTargetGroups int
	failHint                        string
}

func (e *stubErrorELBv2) DescribeTargetHealth(input *elbv2.DescribeTargetHealthInput) (*elbv2.DescribeTargetHealthOutput, error) {
	e.timesCalledDescribeTargetHealth++
	var err error
	if e.failHint == elbv2.ErrCodeTargetGroupNotFoundException {
		err = awserr.New(elbv2.ErrCodeTargetGroupNotFoundException, "failed", fmt.Errorf("it failed"))
	} else {
		err = fmt.Errorf("some other error occured")
	}
	return &elbv2.DescribeTargetHealthOutput{}, err
}

func (e *stubErrorELBv2) DeregisterTargets(input *elbv2.DeregisterTargetsInput) (*elbv2.DeregisterTargetsOutput, error) {
	e.timesCalledDeregisterTargets++
	var err error
	if e.failHint == elbv2.ErrCodeTargetGroupNotFoundException {
		err = awserr.New(elbv2.ErrCodeTargetGroupNotFoundException, "failed", fmt.Errorf("it failed"))
	} else if e.failHint == elbv2.ErrCodeInvalidTargetException {
		err = awserr.New(elbv2.ErrCodeInvalidTargetException, "failed", fmt.Errorf("it failed"))
	} else {
		err = fmt.Errorf("some other error occured")
	}
	return &elbv2.DeregisterTargetsOutput{}, err
}

func (e *stubErrorELBv2) DescribeTargetGroups(input *elbv2.DescribeTargetGroupsInput) (*elbv2.DescribeTargetGroupsOutput, error) {
	e.timesCalledDescribeTargetGroups++
	return &elbv2.DescribeTargetGroupsOutput{TargetGroups: e.targetGroups}, nil
}

func (e *stubErrorELBv2) DescribeTargetGroupsPages(input *elbv2.DescribeTargetGroupsInput, callback func(*elbv2.DescribeTargetGroupsOutput, bool) bool) error {
	page, err := e.DescribeTargetGroups(input)
	if err != nil {
		return err
	}

	callback(page, false)

	return nil
}

func Test_DeregisterTargetWaiterAbort(t *testing.T) {
	t.Log("Test_DeregisterTargetWaiterAbort: should return when event is completed")
	var (
		event               = &LifecycleEvent{}
		arn                 = "arn:aws:elasticloadbalancing:us-west-2:0000000000:targetgroup/targetgroup-name/some-id"
		instanceID          = "i-1234567890"
		port          int64 = 32334
		expectedCalls       = 2
	)

	stubber := &stubELBv2{
		targetHealthDescriptions: []*elbv2.TargetHealthDescription{
			{
				Target: &elbv2.TargetDescription{
					Id:   aws.String(instanceID),
					Port: aws.Int64(port),
				},
				TargetHealth: &elbv2.TargetHealth{
					State: aws.String(elbv2.TargetHealthStateEnumHealthy),
				},
			},
		},
	}

	go _completeEventAfter(event, time.Millisecond*1500)

	err := waitForDeregisterTarget(event, stubber, arn, instanceID, port)
	if err == nil {
		t.Fatalf("Test_DeregisterTargetWaiterAbort: expected error not have occured, %v", err)
	}

	if stubber.timesCalledDescribeTargetHealth != expectedCalls {
		t.Fatalf("Test_DeregisterTargetWaiterAbort: expected timesCalledDescribeTargetHealth: %v, got: %v", expectedCalls, stubber.timesCalledDescribeTargetHealth)
	}
}

func Test_DeregisterTargetWaiterNotFound(t *testing.T) {
	t.Log("Test_DeregisterTargetWaiterNotFound: should return when instance is not found")
	var (
		event               = &LifecycleEvent{}
		arn                 = "arn:aws:elasticloadbalancing:us-west-2:0000000000:targetgroup/targetgroup-name/some-id"
		instanceID          = "i-1234567890"
		port          int64 = 32334
		expectedCalls       = 1
	)

	stubber := &stubELBv2{
		targetHealthDescriptions: []*elbv2.TargetHealthDescription{
			{
				Target: &elbv2.TargetDescription{
					Id:   aws.String("i-11111111"),
					Port: aws.Int64(port),
				},
				TargetHealth: &elbv2.TargetHealth{
					State: aws.String(elbv2.TargetHealthStateEnumHealthy),
				},
			},
		},
	}

	err := waitForDeregisterTarget(event, stubber, arn, instanceID, port)
	if err != nil {
		t.Fatalf("Test_DeregisterTargetWaiterNotFound: expected error not to have occured, %v", err)
	}

	if stubber.timesCalledDescribeTargetHealth != expectedCalls {
		t.Fatalf("Test_DeregisterTargetWaiterNotFound: expected timesCalledDescribeTargetHealth: %v, got: %v", expectedCalls, stubber.timesCalledDescribeTargetHealth)
	}
}

func Test_DeregisterTargetWaiterTimeout(t *testing.T) {
	t.Log("Test_DeregisterTargetWaiterTimeout: should return an error when timeout occurs")
	var (
		event               = &LifecycleEvent{}
		arn                 = "arn:aws:elasticloadbalancing:us-west-2:0000000000:targetgroup/targetgroup-name/some-id"
		instanceID          = "i-1234567890"
		port          int64 = 32334
		expectedCalls       = 3
	)

	stubber := &stubELBv2{
		targetHealthDescriptions: []*elbv2.TargetHealthDescription{
			{
				Target: &elbv2.TargetDescription{
					Id:   aws.String(instanceID),
					Port: aws.Int64(port),
				},
				TargetHealth: &elbv2.TargetHealth{
					State: aws.String(elbv2.TargetHealthStateEnumHealthy),
				},
			},
		},
	}

	err := waitForDeregisterTarget(event, stubber, arn, instanceID, port)
	if err == nil {
		t.Fatalf("Test_DeregisterTargetWaiterNotFound: expected error to have occured, %v", err)
	}

	if stubber.timesCalledDescribeTargetHealth != expectedCalls {
		t.Fatalf("Test_DeregisterTargetWaiterNotFound: expected timesCalledDescribeTargetHealth: %v, got: %v", expectedCalls, stubber.timesCalledDescribeTargetHealth)
	}
}

func Test_DeregisterTargetWaiterFail(t *testing.T) {
	t.Log("Test_DeregisterTargetWaiterFail: should return an error when call fails")
	var (
		event               = &LifecycleEvent{}
		arn                 = "arn:aws:elasticloadbalancing:us-west-2:0000000000:targetgroup/targetgroup-name/some-id"
		instanceID          = "i-1234567890"
		port          int64 = 32334
		expectedCalls       = 1
	)

	stubber := &stubErrorELBv2{
		failHint: elbv2.ErrCodeTargetGroupNotFoundException,
	}

	err := waitForDeregisterTarget(event, stubber, arn, instanceID, port)
	if err == nil {
		t.Fatalf("Test_DeregisterTargetWaiterFail: expected error not to have occured, %v", err)
	}

	if stubber.timesCalledDescribeTargetHealth != expectedCalls {
		t.Fatalf("Test_DeregisterTargetWaiterFail: expected timesCalledDescribeTargetHealth: %v, got: %v", expectedCalls, stubber.timesCalledDescribeTargetHealth)
	}
}

func Test_DeregisterTarget(t *testing.T) {
	t.Log("Test_DeregisterTarget: should be able to deregister an instance")
	var (
		stubber             = &stubELBv2{}
		arn                 = "arn:aws:elasticloadbalancing:us-west-2:0000000000:targetgroup/targetgroup-name/some-id"
		instanceID          = "i-1234567890"
		port          int64 = 32334
		expectedCalls       = 1
	)

	err := deregisterTarget(stubber, arn, instanceID, port)
	if err != nil {
		t.Fatalf("Test_DeregisterInstance: expected error not to have occured, %v", err)
	}

	if stubber.timesCalledDeregisterTargets != expectedCalls {
		t.Fatalf("Test_DeregisterInstance: expected timesCalledDeregisterTargets: %v, got: %v", expectedCalls, stubber.timesCalledDeregisterTargets)
	}
}

func Test_DeregisterTargetNotFoundException(t *testing.T) {
	t.Log("Test_DeregisterTargetNotFoundException: should return error when call fails")
	var (
		stubber             = &stubErrorELBv2{}
		arn                 = "arn:aws:elasticloadbalancing:us-west-2:0000000000:targetgroup/targetgroup-name/some-id"
		instanceID          = "i-1234567890"
		port          int64 = 32334
		expectedCalls       = 1
	)

	stubber.failHint = elbv2.ErrCodeTargetGroupNotFoundException
	err := deregisterTarget(stubber, arn, instanceID, port)
	if err == nil {
		t.Fatalf("Test_DeregisterTargetNotFoundException: expected error not have occured, %v", err)
	}

	if stubber.timesCalledDeregisterTargets != expectedCalls {
		t.Fatalf("Test_DeregisterTargetNotFoundException: expected timesCalledDeregisterTargets: %v, got: %v", expectedCalls, stubber.timesCalledDeregisterTargets)
	}
}

func Test_DeregisterTargetInvalidException(t *testing.T) {
	t.Log("Test_DeregisterTargetInvalidException: should return error when call fails")
	var (
		stubber             = &stubErrorELBv2{}
		arn                 = "arn:aws:elasticloadbalancing:us-west-2:0000000000:targetgroup/targetgroup-name/some-id"
		instanceID          = "i-1234567890"
		port          int64 = 32334
		expectedCalls       = 1
	)

	stubber.failHint = elbv2.ErrCodeInvalidTargetException
	err := deregisterTarget(stubber, arn, instanceID, port)
	if err == nil {
		t.Fatalf("Test_DeregisterTargetInvalidException: expected error not have occured, %v", err)
	}

	if stubber.timesCalledDeregisterTargets != expectedCalls {
		t.Fatalf("Test_DeregisterTargetInvalidException: expected timesCalledDeregisterTargets: %v, got: %v", expectedCalls, stubber.timesCalledDeregisterTargets)
	}
}

func Test_FindInstanceInTargetGroupPositive(t *testing.T) {
	t.Log("Test_FindInstanceInTargetGroupPositive: should be able to find instance in target group if it exists")
	var (
		arn                 = "arn:aws:elasticloadbalancing:us-west-2:0000000000:targetgroup/targetgroup-name/some-id"
		instanceID          = "i-1234567890"
		port          int64 = 32334
		expectedCalls       = 1
	)
	stubber := &stubELBv2{
		targetHealthDescriptions: []*elbv2.TargetHealthDescription{
			{
				Target: &elbv2.TargetDescription{
					Id:   aws.String(instanceID),
					Port: aws.Int64(port),
				},
			},
		},
	}
	found, foundPort, err := findInstanceInTargetGroup(stubber, arn, instanceID)
	if err != nil {
		t.Fatalf("Test_FindInstanceInTargetGroupPositive: expected error not to have occured, %v", err)
	}
	if stubber.timesCalledDescribeTargetHealth != expectedCalls {
		t.Fatalf("expected timesCalledDescribeTargetHealth: %v, got: %v", expectedCalls, stubber.timesCalledDescribeTargetHealth)
	}
	if !found {
		t.Fatalf("Test_FindInstanceInTargetGroupPositive: expected instance to be found")
	}
	if port != foundPort {
		t.Fatalf("Test_FindInstanceInTargetGroupPositive: expected port to be: %v got: %v", port, foundPort)
	}
}

func Test_FindInstanceInTargetGroupNegative(t *testing.T) {
	t.Log("Test_FindInstanceInTargetGroupNegative: should not be able to find instance in target group if it doesnt exists")
	var (
		arn                 = "arn:aws:elasticloadbalancing:us-west-2:0000000000:targetgroup/targetgroup-name/some-id"
		instanceID          = "i-1234567890"
		port          int64 = 32334
		expectedCalls       = 1
	)
	stubber := &stubELBv2{
		targetHealthDescriptions: []*elbv2.TargetHealthDescription{
			{
				Target: &elbv2.TargetDescription{
					Id:   aws.String("i-222333444"),
					Port: aws.Int64(port),
				},
			},
		},
	}
	found, _, err := findInstanceInTargetGroup(stubber, arn, instanceID)
	if err != nil {
		t.Fatalf("Test_FindInstanceInTargetGroupPositive: expected error not to have occured, %v", err)
	}
	if stubber.timesCalledDescribeTargetHealth != expectedCalls {
		t.Fatalf("expected timesCalledDescribeTargetHealth: %v, got: %v", expectedCalls, stubber.timesCalledDescribeTargetHealth)
	}
	if found {
		t.Fatalf("Test_FindInstanceInTargetGroupPositive: expected instance not to be found")
	}
}

func Test_FindInstanceInTargetGroupError(t *testing.T) {
	t.Log("Test_FindInstanceInTargetGroupError: should return error if call fails")
	var (
		arn           = "arn:aws:elasticloadbalancing:us-west-2:0000000000:targetgroup/targetgroup-name/some-id"
		instanceID    = "i-1234567890"
		expectedCalls = 1
		stubber       = &stubErrorELBv2{}
	)

	stubber.failHint = elbv2.ErrCodeTargetGroupNotFoundException

	found, _, err := findInstanceInTargetGroup(stubber, arn, instanceID)
	if err == nil {
		t.Fatalf("Test_FindInstanceInTargetGroupError: expected error to have occured, got: %v", err)
	}
	if stubber.timesCalledDescribeTargetHealth != expectedCalls {
		t.Fatalf("Test_FindInstanceInTargetGroupError: %v, got: %v", expectedCalls, stubber.timesCalledDescribeTargetHealth)
	}
	if found {
		t.Fatalf("Test_FindInstanceInTargetGroupError: expected instance not to be found")
	}
}
