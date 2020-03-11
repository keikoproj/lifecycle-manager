package enroll

import (
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/autoscaling"
	"github.com/aws/aws-sdk-go/service/autoscaling/autoscalingiface"
	"github.com/aws/aws-sdk-go/service/iam"
	"github.com/aws/aws-sdk-go/service/iam/iamiface"
	"github.com/aws/aws-sdk-go/service/sqs"
	"github.com/aws/aws-sdk-go/service/sqs/sqsiface"
	"github.com/keikoproj/lifecycle-manager/pkg/log"
	"github.com/pkg/errors"
)

const (
	terminationTransitionName   = "autoscaling:EC2_INSTANCE_TERMINATING"
	defaultHookName             = "lifecycle-manager"
	notificationRoleDescription = `Role used by lifecycle-manager for sending hooks from autoscaling to SQS`
	notificationPolicyARN       = "arn:aws:iam::aws:policy/service-role/AutoScalingNotificationAccessRole"
)

// EnrollmentAuthenticator holds clients for the enroll command
type EnrollmentAuthenticator struct {
	ScalingGroupClient autoscalingiface.AutoScalingAPI
	SQSClient          sqsiface.SQSAPI
	IAMClient          iamiface.IAMAPI
}

type EnrollmentContext struct {
	QueueName            string
	Region               string
	NotificationRoleName string
	TargetScalingGroups  []string
	HeartbeatTimeout     uint
	Authenticator        EnrollmentAuthenticator
	QueueURL             string
	QueueARN             string
	RoleARN              string
	Overwrite            bool
}

type Worker struct {
	authenticator EnrollmentAuthenticator
	context       *EnrollmentContext
}

func New(auth EnrollmentAuthenticator, ctx *EnrollmentContext) *Worker {
	return &Worker{
		authenticator: auth,
		context:       ctx,
	}
}

func (w *Worker) Start() {
	var (
		ctx = w.context
	)

	log.Infof("starting enrollment for scaling groups %+v", ctx.TargetScalingGroups)

	if err := w.CreateNotificationRole(); err != nil {
		log.Fatal(err)
	}

	if err := w.CreateSQSQueue(); err != nil {
		log.Fatal(err)
	}

	for _, scalingGroup := range ctx.TargetScalingGroups {
		if err := w.CreateLifecycleHook(scalingGroup); err != nil {
			log.Fatal(err)
		}
	}

	log.Infof("successfully enrolled %v scaling groups", len(ctx.TargetScalingGroups))
	log.Infof("Queue Name: %v", ctx.QueueName)
	log.Infof("Queue URL: %v", ctx.QueueURL)
}

func (w *Worker) CreateLifecycleHook(scalingGroup string) error {
	var (
		ASGClient = w.authenticator.ScalingGroupClient
		ctx       = w.context
	)

	crInput := &autoscaling.PutLifecycleHookInput{
		AutoScalingGroupName:  aws.String(scalingGroup),
		HeartbeatTimeout:      aws.Int64(int64(ctx.HeartbeatTimeout)),
		LifecycleHookName:     aws.String(defaultHookName),
		LifecycleTransition:   aws.String(terminationTransitionName),
		NotificationTargetARN: aws.String(ctx.QueueARN),
		RoleARN:               aws.String(ctx.RoleARN),
	}

	log.Infof("creating lifecycle hook for '%v'", scalingGroup)
	if _, err := ASGClient.PutLifecycleHook(crInput); err != nil {
		return errors.Errorf("failed to put lifecycle hook: %v", err)
	}
	return nil
}

func (w *Worker) CreateSQSQueue() error {
	var (
		SQSClient = w.authenticator.SQSClient
		ctx       = w.context
	)

	crInput := &sqs.CreateQueueInput{
		QueueName: aws.String(ctx.QueueName),
	}

	log.Infof("creating SQS queue '%v'", ctx.QueueName)
	out, err := SQSClient.CreateQueue(crInput)
	if err != nil {
		return errors.Errorf("failed to create SQS queue: %v", err)
	}

	ctx.QueueURL = aws.StringValue(out.QueueUrl)

	desInput := &sqs.GetQueueAttributesInput{
		QueueUrl:       out.QueueUrl,
		AttributeNames: aws.StringSlice([]string{"QueueArn"}),
	}

	attr, err := SQSClient.GetQueueAttributes(desInput)
	if err != nil {
		return errors.Errorf("failed get queue attribute: %v", err)
	}

	ctx.QueueARN = aws.StringValue(attr.Attributes["QueueArn"])
	log.Infof("created queue '%v'", ctx.QueueARN)
	return nil
}

func (w *Worker) CreateNotificationRole() error {
	var (
		IAMClient    = w.authenticator.IAMClient
		ctx          = w.context
		alreadyExist bool
	)

	log.Infof("creating notification role '%v'", ctx.NotificationRoleName)
	crInput := &iam.CreateRoleInput{
		AssumeRolePolicyDocument: getAssumeRolePolicyDocument(),
		Description:              aws.String(notificationRoleDescription),
		RoleName:                 aws.String(ctx.NotificationRoleName),
	}

	out, err := IAMClient.CreateRole(crInput)
	if err != nil {
		if awsErr, ok := err.(awserr.Error); ok {
			switch awsErr.Code() {
			case iam.ErrCodeEntityAlreadyExistsException:
				alreadyExist = true
			default:
				return errors.Errorf("failed to create notification role: %v", awsErr.Message())
			}
		}
	}

	if alreadyExist {
		if !ctx.Overwrite {
			log.Warn("set flag --overwrite for re-using existing role")
			return errors.Errorf("failed to create notification role: %v", err)
		}
		log.Infof("notification role '%v' already exist, updating...", ctx.NotificationRoleName)
		input := &iam.GetRoleInput{
			RoleName: aws.String(ctx.NotificationRoleName),
		}
		out, err := IAMClient.GetRole(input)
		if err != nil {
			return errors.Errorf("failed to get exisinting notification role: %v", err)
		}
		ctx.RoleARN = aws.StringValue(out.Role.Arn)
	} else {
		ctx.RoleARN = aws.StringValue(out.Role.Arn)
	}

	atInput := &iam.AttachRolePolicyInput{
		PolicyArn: aws.String(notificationPolicyARN),
		RoleName:  aws.String(ctx.NotificationRoleName),
	}

	log.Infof("attaching notification policy '%v'", notificationPolicyARN)
	if _, err := IAMClient.AttachRolePolicy(atInput); err != nil {
		return errors.Errorf("failed to attach notification role: %v", err)
	}

	log.Infof("created notification role '%v'", ctx.RoleARN)

	return nil
}

func getAssumeRolePolicyDocument() *string {
	doc := `{
		"Version": "2012-10-17",
		"Statement": [
			{
				"Effect": "Allow",
				"Principal": {
			  		"Service": "autoscaling.amazonaws.com"
				},
				"Action": "sts:AssumeRole"
		  	}
		]
	}`
	return aws.String(doc)
}
