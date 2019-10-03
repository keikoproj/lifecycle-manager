package cmd

import (
	"os"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/autoscaling"
	"github.com/aws/aws-sdk-go/service/autoscaling/autoscalingiface"
	"github.com/aws/aws-sdk-go/service/sqs"
	"github.com/aws/aws-sdk-go/service/sqs/sqsiface"
	"github.com/aws/aws-sdk-go/service/ssm"
	"github.com/aws/aws-sdk-go/service/ssm/ssmiface"
	"github.com/keikoproj/lifecycle-manager/pkg/log"
	"github.com/keikoproj/lifecycle-manager/pkg/service"
	"github.com/spf13/cobra"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

var (
	localMode        string
	region           string
	queueName        string
	kubectlLocalPath string
	nodeName         string
	ssmDocument      string
	logLevel         string

	postDrainCommand bool
	deleteNode       bool

	drainRetryIntervalSeconds int
	drainTimeoutSeconds       int
	pollingIntervalSeconds    int
)

// serveCmd represents the serve command
var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "start the lifecycle-manager service",
	Long:  `Start watching lifecycle events for a given queue`,
	Run: func(cmd *cobra.Command, args []string) {
		// argument validation
		validate()
		log.SetLevel(logLevel)

		// prepare auth clients
		auth := service.Authenticator{
			ScalingGroupClient: newASGClient(region),
			SQSClient:          newSQSClient(region),
			SSMClient:          newSSMClient(region),
			KubernetesClient:   newKubernetesClient(localMode),
		}

		// prepare runtime context
		context := service.ManagerContext{
			KubectlLocalPath:          kubectlLocalPath,
			QueueName:                 queueName,
			DrainTimeoutSeconds:       int64(drainTimeoutSeconds),
			PollingIntervalSeconds:    int64(pollingIntervalSeconds),
			DrainRetryIntervalSeconds: int64(drainRetryIntervalSeconds),
			RunCommand:                postDrainCommand,
			CommandDocument:           ssmDocument,
			DeleteNode:                deleteNode,
			Region:                    region,
		}

		s := service.New(auth, context)
		s.Start()
	},
}

func init() {
	rootCmd.AddCommand(serveCmd)
	serveCmd.Flags().StringVar(&localMode, "local-mode", "", "absolute path to kubeconfig")
	serveCmd.Flags().StringVar(&region, "region", "", "aws region to operate in")
	serveCmd.Flags().StringVar(&queueName, "queue-name", "", "the name of the sqs queue to consume lifecycle hooks from")
	serveCmd.Flags().StringVar(&kubectlLocalPath, "kubectl-path", "/usr/local/bin/kubectl", "the path to kubectl binary")
	serveCmd.Flags().IntVar(&drainTimeoutSeconds, "drain-timeout", 300, "hard time limit for drain")
	serveCmd.Flags().IntVar(&drainRetryIntervalSeconds, "drain-interval", 30, "interval in seconds for which to retry draining")
	serveCmd.Flags().IntVar(&pollingIntervalSeconds, "polling-interval", 10, "interval in seconds for which to poll sqs")
	serveCmd.Flags().StringVar(&logLevel, "log-level", "info", "set logging verbosity (info, warning, or debug)")
	serveCmd.Flags().BoolVar(&postDrainCommand, "post-drain-command", false, "submit an aws ssm command after draining")
	serveCmd.Flags().BoolVar(&deleteNode, "delete-node-object", false, "runs in the context of post-drain-command, will delete node object after command execution")
	serveCmd.Flags().StringVar(&ssmDocument, "ssm-document", "", "name of an ssm document to use")
}

func validate() {
	if localMode != "" {
		if _, err := os.Stat(localMode); os.IsNotExist(err) {
			log.Fatalf("provided kubeconfig path does not exist")
		}
	}

	if kubectlLocalPath != "" {
		if _, err := os.Stat(kubectlLocalPath); os.IsNotExist(err) {
			log.Fatalf("provided kubectl path does not exist")
		}
	} else {
		log.Fatalf("must provide kubectl path")
	}

	if region == "" {
		log.Fatalf("must provide valid aws region name")
	}

	if queueName == "" {
		log.Fatalf("must provide valid sqs queue name")
	}

	if postDrainCommand {
		if ssmDocument == "" {
			log.Fatalf("must provide an ssm document name")
		}
	}

	if deleteNode && !postDrainCommand {
		log.Fatalf("--delete-node-object can only be enabled in the context of --post-drain-command")
	}
}

func newKubernetesClient(localMode string) *kubernetes.Clientset {
	var config *rest.Config
	var err error

	if localMode != "" {
		// use kubeconfig
		config, err = clientcmd.BuildConfigFromFlags("", localMode)
		if err != nil {
			log.Fatalf("cannot load kubernetes config from '%v'", localMode)
		}
	} else {
		// use InCluster auth
		config, err = rest.InClusterConfig()
		if err != nil {
			log.Fatalln("cannot load kubernetes config from InCluster")
		}
	}
	return kubernetes.NewForConfigOrDie(config)
}

func newSQSClient(region string) sqsiface.SQSAPI {
	sess, err := session.NewSession(&aws.Config{
		Region: aws.String(region)},
	)
	if err != nil {
		log.Fatalf("failed to create sqs client, %v", err)
	}
	return sqs.New(sess)
}

func newSSMClient(region string) ssmiface.SSMAPI {
	sess, err := session.NewSession(&aws.Config{
		Region: aws.String(region)},
	)
	if err != nil {
		log.Fatalf("failed to create ssm client, %v", err)
	}
	return ssm.New(sess)
}

func newASGClient(region string) autoscalingiface.AutoScalingAPI {
	sess, err := session.NewSession(&aws.Config{
		Region: aws.String(region)},
	)
	if err != nil {
		log.Fatalf("failed to create asg client, %v", err)
	}
	return autoscaling.New(sess)
}
