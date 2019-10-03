package service

import (
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/ssm"
	"github.com/aws/aws-sdk-go/service/ssm/ssmiface"
	"github.com/keikoproj/lifecycle-manager/pkg/log"
	"github.com/pkg/errors"
)

var (
	// MaxWaitRetries is the default maximum number of retries on command-wait
	MaxWaitRetries = 6
	// CommandWaitIntervalSeconds is the wait interval between every check
	CommandWaitIntervalSeconds = 10
	// MaxCommandRetries is the number of time to retry the script if it fails
	MaxCommandRetries = "3"
	// CommandComment is the comment displayed for each command invocation
	CommandComment = "post-drain lifecycle-manager action"
	// ErrMaxRetriesExceeded is an error for when the maximum number of wait attempts is exceeded
	ErrMaxRetriesExceeded = errors.New("ErrMaxRetriesExceeded: maximum number of command wait retries exceeded")
	// ErrExecutionFailed is an error for when the ssm command execution results in failrue
	ErrExecutionFailed = errors.New("ErrExecutionFailed: the command execution has failed")
)

func sendCommand(ssmClient ssmiface.SSMAPI, document, target string) (string, error) {
	input := &ssm.SendCommandInput{
		Comment:      aws.String(CommandComment),
		DocumentName: aws.String(document),
		InstanceIds:  aws.StringSlice([]string{target}),
		MaxErrors:    aws.String(MaxCommandRetries),
	}
	output, err := ssmClient.SendCommand(input)
	if err != nil {
		return "", err
	}
	return aws.StringValue(output.Command.CommandId), nil
}

func waitForCommand(ssmClient ssmiface.SSMAPI, commandID, instanceID string) error {
	var retryCount int

	for {
		if retryCount == MaxWaitRetries {
			return ErrMaxRetriesExceeded
		}
		retryCount++
		input := &ssm.GetCommandInvocationInput{
			CommandId:  aws.String(commandID),
			InstanceId: aws.String(instanceID),
		}
		output, err := ssmClient.GetCommandInvocation(input)
		if err != nil {
			if awsErr, ok := err.(awserr.Error); ok {
				switch awsErr.Code() {
				case ssm.ErrCodeInvocationDoesNotExist:
					log.Infof("invocation for %v was not found, will retry", commandID)
					time.Sleep(time.Duration(CommandWaitIntervalSeconds) * time.Second)
				default:
					return err
				}
			} else {
				return err
			}
		}
		statusDetail := aws.StringValue(output.StatusDetails)
		responseCode := aws.Int64Value(output.ResponseCode)
		stdout, stderr := aws.StringValue(output.StandardOutputContent), aws.StringValue(output.StandardErrorContent)

		if aws.StringValue(output.ExecutionEndDateTime) != "" {
			switch responseCode {
			case -1:
				log.Infof("command %v has not started executing: %v", commandID, statusDetail)
				time.Sleep(time.Duration(CommandWaitIntervalSeconds) * time.Second)
			case 0:
				log.Infof("command %v has executed successfully: %v", commandID, statusDetail)
				return nil
			default:
				log.Infof("command %v has failed execution: %v", commandID, statusDetail)
				log.Infof("command %v stdout: %v, stderr: %v", commandID, stdout, stderr)
				return ErrExecutionFailed
			}
		} else {
			log.Infof("command %v is still executing", commandID)
			time.Sleep(time.Duration(CommandWaitIntervalSeconds) * time.Second)
		}
	}
}
