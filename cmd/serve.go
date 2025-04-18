package cmd

import (
	"fmt"
	"os"
	"time"

	"github.com/aws/aws-sdk-go/aws/client"
	"github.com/keikoproj/aws-sdk-go-cache/cache"
	"github.com/keikoproj/lifecycle-manager/pkg/log"
	"github.com/keikoproj/lifecycle-manager/pkg/service"
	"github.com/spf13/cobra"
	"golang.org/x/sync/semaphore"
)

const (
	CacheDefaultTTL           time.Duration = time.Second * 0
	DescribeTargetHealthTTL   time.Duration = 120 * time.Second
	DescribeInstanceHealthTTL time.Duration = 120 * time.Second
	DescribeTargetGroupsTTL   time.Duration = 300 * time.Second
	DescribeLoadBalancersTTL  time.Duration = 300 * time.Second
	CacheMaxItems             int64         = 5000
	CacheItemsToPrune         uint32        = 500
)

var (
	localMode                  string
	region                     string
	queueName                  string
	kubectlLocalPath           string
	nodeName                   string
	logLevel                   string
	deregisterTargetGroups     bool
	deregisterTargetTypes      []string
	refreshExpiredCredentials  bool
	drainRetryIntervalSeconds  int
	maxDrainConcurrency        int64
	drainTimeoutSeconds        int
	drainTimeoutUnknownSeconds int
	drainRetryAttempts         int
	pollingIntervalSeconds     int
	maxTimeToProcessSeconds    int64

	// DefaultRetryer is the default retry configuration for some AWS API calls
	DefaultRetryer = client.DefaultRetryer{
		NumMaxRetries:    250,
		MinThrottleDelay: time.Second * 5,
		MaxThrottleDelay: time.Second * 60,
		MinRetryDelay:    time.Second * 1,
		MaxRetryDelay:    time.Second * 5,
	}
)

// serveCmd represents the serve command
var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "start the lifecycle-manager service",
	Long:  `Start watching lifecycle events for a given queue`,
	Run: func(cmd *cobra.Command, args []string) {
		// argument validation
		validateServe()
		log.SetLevel(logLevel)
		cacheCfg := cache.NewConfig(CacheDefaultTTL, 1*time.Hour, CacheMaxItems, CacheItemsToPrune)

		// prepare auth clients
		auth := service.Authenticator{
			ScalingGroupClient: newASGClient(region),
			SQSClient:          newSQSClient(region),
			ELBv2Client:        newELBv2Client(region, cacheCfg),
			ELBClient:          newELBClient(region, cacheCfg),
			KubernetesClient:   newKubernetesClient(localMode),
		}

		// prepare runtime context
		context := service.ManagerContext{
			CacheConfig:                cacheCfg,
			KubectlLocalPath:           kubectlLocalPath,
			QueueName:                  queueName,
			DrainTimeoutSeconds:        int64(drainTimeoutSeconds),
			DrainTimeoutUnknownSeconds: int64(drainTimeoutUnknownSeconds),
			PollingIntervalSeconds:     int64(pollingIntervalSeconds),
			DrainRetryIntervalSeconds:  int64(drainRetryIntervalSeconds),
			MaxDrainConcurrency:        semaphore.NewWeighted(maxDrainConcurrency),
			MaxTimeToProcessSeconds:    int64(maxTimeToProcessSeconds),
			DrainRetryAttempts:         uint(drainRetryAttempts),
			Region:                     region,
			WithDeregister:             deregisterTargetGroups,
			DeregisterTargetTypes:      deregisterTargetTypes,
		}

		s := service.New(auth, context)
		s.Start()
	},
}

func init() {
	rootCmd.AddCommand(serveCmd)
	serveCmd.Flags().StringVar(&localMode, "local-mode", "", "absolute path to kubeconfig")
	serveCmd.Flags().StringVar(&region, "region", "", "AWS region to operate in")
	serveCmd.Flags().StringVar(&queueName, "queue-name", "", "the name of the SQS queue to consume lifecycle hooks from")
	serveCmd.Flags().StringVar(&kubectlLocalPath, "kubectl-path", "/usr/local/bin/kubectl", "the path to kubectl binary")
	serveCmd.Flags().StringVar(&logLevel, "log-level", "info", "the logging level (info, warning, debug)")
	serveCmd.Flags().Int64Var(&maxDrainConcurrency, "max-drain-concurrency", 32, "maximum number of node drains to process in parallel")
	serveCmd.Flags().Int64Var(&maxTimeToProcessSeconds, "max-time-to-process", 3600, "max time to spend processing an event")
	serveCmd.Flags().IntVar(&drainTimeoutSeconds, "drain-timeout", 300, "hard time limit for draining healthy nodes")
	serveCmd.Flags().IntVar(&drainTimeoutUnknownSeconds, "drain-timeout-unknown", 30, "hard time limit for draining nodes that are in unknown state")
	serveCmd.Flags().IntVar(&drainRetryIntervalSeconds, "drain-interval", 30, "interval in seconds for which to retry draining")
	serveCmd.Flags().IntVar(&drainRetryAttempts, "drain-retries", 3, "number of times to retry the node drain operation")
	serveCmd.Flags().IntVar(&pollingIntervalSeconds, "polling-interval", 10, "interval in seconds for which to poll SQS")
	serveCmd.Flags().BoolVar(&deregisterTargetGroups, "with-deregister", true, "try to deregister deleting instance from target groups")
	serveCmd.Flags().StringSliceVar(&deregisterTargetTypes, "deregister-target-types", []string{service.TargetTypeClassicELB.String(), service.TargetTypeTargetGroup.String()},
		fmt.Sprintf("comma separated list of target types to deregister instance from (%s, %s)", service.TargetTypeClassicELB.String(), service.TargetTypeTargetGroup.String()))
	serveCmd.Flags().BoolVar(&refreshExpiredCredentials, "refresh-expired-credentials", false, "refreshes expired credentials (requires shared credentials file)")
}

func validateServe() {
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
		log.Fatalf("must provide valid AWS region name")
	}

	if queueName == "" {
		log.Fatalf("must provide valid SQS queue name")
	}

	if maxDrainConcurrency < 1 {
		log.Fatalf("--max-drain-concurrency must be set to a value higher than 0")
	}
}
