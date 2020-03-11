package cmd

import (
	"github.com/keikoproj/lifecycle-manager/pkg/enroll"
	"github.com/keikoproj/lifecycle-manager/pkg/log"
	"github.com/spf13/cobra"
)

var (
	overwrite            bool
	enrollRegion         string
	enrollQueueName      string
	notificationRoleName string
	heartbeatTimeout     uint
	targetScalingGroups  []string
)

// enrollCmd represents the enroll command
var enrollCmd = &cobra.Command{
	Use:   "enroll",
	Short: "creates an SQS queue and enrolls ASGs to send hooks to it",
	Long: `enroll does the initial required setup to start running lifecycle-manager, you can use
			this CLI as a pre-setup step to running the controller`,
	Run: func(cmd *cobra.Command, args []string) {
		// argument validation
		validateEnroll()
		log.SetLevel(logLevel)

		// prepare auth clients
		auth := enroll.EnrollmentAuthenticator{
			ScalingGroupClient: newASGClient(enrollRegion),
			SQSClient:          newSQSClient(enrollRegion),
			IAMClient:          newIAMClient(enrollRegion),
		}

		// prepare runtime context
		context := &enroll.EnrollmentContext{
			Region:               enrollRegion,
			QueueName:            enrollQueueName,
			NotificationRoleName: notificationRoleName,
			TargetScalingGroups:  targetScalingGroups,
			HeartbeatTimeout:     heartbeatTimeout,
			Overwrite:            overwrite,
		}

		e := enroll.New(auth, context)
		e.Start()
	},
}

func init() {
	rootCmd.AddCommand(enrollCmd)
	enrollCmd.Flags().BoolVar(&overwrite, "overwrite", false, "update resources if they already exist")
	enrollCmd.Flags().StringVar(&enrollRegion, "region", "", "AWS region to operate in")
	enrollCmd.Flags().StringVar(&enrollQueueName, "queue-name", "", "the name of the SQS queue to create")
	enrollCmd.Flags().StringVar(&notificationRoleName, "notification-role-name", "", "the name of the notification IAM role to create")
	enrollCmd.Flags().StringSliceVar(&targetScalingGroups, "target-scaling-groups", []string{}, "comma separated list of auto scaling group names")
	enrollCmd.Flags().UintVar(&heartbeatTimeout, "heartbeat-timeout", 300, "lifecycle hook heartbeat timeout")
}

func validateEnroll() {
	if enrollRegion == "" {
		log.Fatalf("--region was not provided")
	}

	if enrollQueueName == "" {
		log.Fatalf("--queue-name was not provided")
	}

	if notificationRoleName == "" {
		log.Fatalf("--notification-role-name was not provided")
	}

	if len(targetScalingGroups) == 0 {
		log.Fatalf("--target-scaling-groups was not provided")
	}
}
