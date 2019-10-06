package service

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go/aws/request"

	"github.com/aws/aws-sdk-go/aws"
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

func Test_DeregisterInstance(t *testing.T) {
	t.Log("Test_DeregisterInstance: should be able to deregister an instance")
	var (
		stubber             = &stubELBv2{}
		arn                 = "arn:aws:elasticloadbalancing:us-west-2:0000000000:targetgroup/targetgroup-name/some-id"
		instanceID          = "i-1234567890"
		port          int64 = 32334
		expectedCalls       = 1
	)

	err := deregisterInstance(stubber, arn, instanceID, port)
	if err != nil {
		t.Fatalf("Test_DeregisterInstance: expected error not to have occured, %v", err)
	}

	if stubber.timesCalledDeregisterTargets != expectedCalls {
		t.Fatalf("Test_DeregisterInstance: expected timesCalledDeregisterTargets: %v, got: %v", expectedCalls, stubber.timesCalledDeregisterTargets)
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
