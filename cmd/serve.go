package cmd

import (
	"os"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/client"
	"github.com/aws/aws-sdk-go/aws/request"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/autoscaling"
	"github.com/aws/aws-sdk-go/service/autoscaling/autoscalingiface"
	"github.com/aws/aws-sdk-go/service/elb"
	"github.com/aws/aws-sdk-go/service/elb/elbiface"
	"github.com/aws/aws-sdk-go/service/elbv2"
	"github.com/aws/aws-sdk-go/service/elbv2/elbv2iface"
	"github.com/aws/aws-sdk-go/service/sqs"
	"github.com/aws/aws-sdk-go/service/sqs/sqsiface"
	"github.com/keikoproj/aws-sdk-go-cache/cache"
	"github.com/keikoproj/lifecycle-manager/pkg/log"
	"github.com/keikoproj/lifecycle-manager/pkg/service"
	"github.com/spf13/cobra"
	"golang.org/x/sync/semaphore"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
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
	drainRetryIntervalSeconds  int
	maxDrainConcurrency        int64
	drainTimeoutSeconds        int
	drainTimeoutUnknownSeconds int
	pollingIntervalSeconds     int

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
		validate()
		log.SetLevel(logLevel)
		cacheCfg := cache.NewConfig(CacheDefaultTTL, CacheMaxItems, CacheItemsToPrune)

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
			Region:                     region,
			WithDeregister:             deregisterTargetGroups,
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
	serveCmd.Flags().IntVar(&drainTimeoutSeconds, "drain-timeout", 300, "hard time limit for draining healthy nodes")
	serveCmd.Flags().IntVar(&drainTimeoutUnknownSeconds, "drain-timeout-unknown", 30, "hard time limit for draining nodes that are in unknown state")
	serveCmd.Flags().IntVar(&drainRetryIntervalSeconds, "drain-interval", 30, "interval in seconds for which to retry draining")
	serveCmd.Flags().IntVar(&pollingIntervalSeconds, "polling-interval", 10, "interval in seconds for which to poll SQS")
	serveCmd.Flags().BoolVar(&deregisterTargetGroups, "with-deregister", true, "try to deregister deleting instance from target groups")
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
		log.Fatalf("must provide valid AWS region name")
	}

	if queueName == "" {
		log.Fatalf("must provide valid SQS queue name")
	}

	if maxDrainConcurrency < 1 {
		log.Fatalf("--max-drain-concurrency must be set to a value higher than 0")
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

func newELBv2Client(region string, cacheCfg *cache.Config) elbv2iface.ELBV2API {
	config := aws.NewConfig().WithRegion(region)
	config = config.WithCredentialsChainVerboseErrors(true)
	config = request.WithRetryer(config, log.NewRetryLogger(DefaultRetryer))
	sess, err := session.NewSession(config)
	if err != nil {
		log.Fatalf("failed to create elbv2 client, %v", err)
	}
	cache.AddCaching(sess, cacheCfg)
	cacheCfg.SetCacheTTL("elasticloadbalancing", "DescribeTargetHealth", DescribeTargetHealthTTL)
	cacheCfg.SetCacheTTL("elasticloadbalancing", "DescribeTargetGroups", DescribeTargetGroupsTTL)
	cacheCfg.SetCacheMutating("elasticloadbalancing", "DeregisterTargets", false)
	sess.Handlers.Complete.PushFront(func(r *request.Request) {
		ctx := r.HTTPRequest.Context()
		log.Debugf("cache hit => %v, service => %s.%s",
			cache.IsCacheHit(ctx),
			r.ClientInfo.ServiceName,
			r.Operation.Name,
		)
	})
	return elbv2.New(sess)
}

func newELBClient(region string, cacheCfg *cache.Config) elbiface.ELBAPI {
	config := aws.NewConfig().WithRegion(region)
	config = config.WithCredentialsChainVerboseErrors(true)
	config = request.WithRetryer(config, log.NewRetryLogger(DefaultRetryer))
	sess, err := session.NewSession(config)
	if err != nil {
		log.Fatalf("failed to create elb client, %v", err)
	}
	cache.AddCaching(sess, cacheCfg)
	cacheCfg.SetCacheTTL("elasticloadbalancing", "DescribeInstanceHealth", DescribeInstanceHealthTTL)
	cacheCfg.SetCacheTTL("elasticloadbalancing", "DescribeLoadBalancers", DescribeLoadBalancersTTL)
	cacheCfg.SetCacheMutating("elasticloadbalancing", "DeregisterInstancesFromLoadBalancer", false)
	sess.Handlers.Complete.PushFront(func(r *request.Request) {
		ctx := r.HTTPRequest.Context()
		log.Debugf("cache hit => %v, service => %s.%s",
			cache.IsCacheHit(ctx),
			r.ClientInfo.ServiceName,
			r.Operation.Name,
		)
	})
	return elb.New(sess)
}

func newSQSClient(region string) sqsiface.SQSAPI {
	config := aws.NewConfig().WithRegion(region)
	config = config.WithCredentialsChainVerboseErrors(true)
	config = request.WithRetryer(config, log.NewRetryLogger(DefaultRetryer))
	sess, err := session.NewSession(config)
	if err != nil {
		log.Fatalf("failed to create sqs client, %v", err)
	}
	return sqs.New(sess)
}

func newASGClient(region string) autoscalingiface.AutoScalingAPI {
	config := aws.NewConfig().WithRegion(region)
	config = config.WithCredentialsChainVerboseErrors(true)
	config = request.WithRetryer(config, log.NewRetryLogger(DefaultRetryer))
	sess, err := session.NewSession(config)
	if err != nil {
		log.Fatalf("failed to create asg client, %v", err)
	}
	return autoscaling.New(sess)
}
