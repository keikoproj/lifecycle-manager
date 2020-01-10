package service

import (
	"errors"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/elbv2"
	"github.com/aws/aws-sdk-go/service/elbv2/elbv2iface"

	"github.com/keikoproj/lifecycle-manager/pkg/log"
)

func waitForDeregisterTarget(elbClient elbv2iface.ELBV2API, arn, instanceID string, port int64) error {
	var (
		DelayIntervalSeconds int64 = 30
		MaxAttempts                = 500
		found                bool
	)

	input := &elbv2.DescribeTargetHealthInput{
		TargetGroupArn: aws.String(arn),
	}

	for i := 0; i < MaxAttempts; i++ {
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
			log.Infof("target %v not found in target group %v", instanceID, arn)
			return nil
		}
		log.Infof("target %v not yet deregistered from target group %v, waiting %vs", instanceID, arn, DelayIntervalSeconds)
		time.Sleep(time.Second * time.Duration(DelayIntervalSeconds))
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
		log.Infof("failed finding instance %v in target group %v: %v", instanceID, arn, err.Error())
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

func deregisterTarget(elbClient elbv2iface.ELBV2API, arn, instanceID string, port int64) error {
	input := &elbv2.DeregisterTargetsInput{
		Targets: []*elbv2.TargetDescription{
			{
				Id:   aws.String(instanceID),
				Port: aws.Int64(port),
			},
		},
		TargetGroupArn: aws.String(arn),
	}

	log.Infof("deregistering %v from %v", instanceID, arn)
	_, err := elbClient.DeregisterTargets(input)
	if err != nil {
		return err
	}
	return nil
}

func getTargetGroupArns(targets []*elbv2.TargetGroup) []string {
	arns := []string{}
	for _, tg := range targets {
		arns = append(arns, aws.StringValue(tg.TargetGroupArn))
	}
	return arns
}

func isTargetGroupInScope(tags []*elbv2.TagDescription, arn, tagKey, tagValue string) bool {
	for _, tag := range tags {
		if aws.StringValue(tag.ResourceArn) == arn {
			for _, resourceTag := range tag.Tags {
				if aws.StringValue(resourceTag.Key) == tagKey && aws.StringValue(resourceTag.Value) == tagValue {
					return true
				}
			}
		}
	}
	return false
}
