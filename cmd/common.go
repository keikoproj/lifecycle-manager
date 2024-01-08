package cmd

import (
	"os"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/defaults"
	"github.com/aws/aws-sdk-go/aws/request"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/autoscaling"
	"github.com/aws/aws-sdk-go/service/autoscaling/autoscalingiface"
	"github.com/aws/aws-sdk-go/service/elb"
	"github.com/aws/aws-sdk-go/service/elb/elbiface"
	"github.com/aws/aws-sdk-go/service/elbv2"
	"github.com/aws/aws-sdk-go/service/elbv2/elbv2iface"
	"github.com/aws/aws-sdk-go/service/iam"
	"github.com/aws/aws-sdk-go/service/iam/iamiface"
	"github.com/aws/aws-sdk-go/service/sqs"
	"github.com/aws/aws-sdk-go/service/sqs/sqsiface"
	"github.com/keikoproj/aws-sdk-go-cache/cache"
	"github.com/keikoproj/lifecycle-manager/pkg/log"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

func newKubernetesClient(localMode string) *kubernetes.Clientset {
	var config *rest.Config
	var err error

	if localMode != "" {
		// use kubeconfig
		config, err = clientcmd.BuildConfigFromFlags("", localMode)
		if err != nil {
			log.Fatalf("cannot load kubernetes config from '%v', Err=%s", localMode, err)
		}
	} else {
		// use InCluster auth
		config, err = rest.InClusterConfig()
		if err != nil {
			log.Fatalln("cannot load kubernetes config from InCluster")
		}
		config.QPS = 600
		config.Burst = 2000
	}
	return kubernetes.NewForConfigOrDie(config)
}

func newIAMClient(region string) iamiface.IAMAPI {
	config := aws.NewConfig().WithRegion(region)
	config = config.WithCredentialsChainVerboseErrors(true)
	sess, err := session.NewSession(config)
	if err != nil {
		log.Fatalf("failed to create iam client, %v", err)
	}
	return iam.New(sess)
}

func newAWSSession(region string) (*session.Session, error) {
	config := aws.NewConfig().WithRegion(region)
	config = config.WithCredentialsChainVerboseErrors(true)

	if refreshExpiredCredentials {
		filename := os.Getenv("AWS_SHARED_CREDENTIALS_FILE")
		if filename == "" {
			filename = defaults.SharedCredentialsFilename()
		}

		profile := os.Getenv("AWS_PROFILE")
		if profile == "" {
			profile = "default"
		}

		// When corehandlers.AfterRetryHandler calls Config.Credentials.Expire,
		// the SharedCredentialsProvider forces refreshing credentials from file.
		// With the SDK's default credential chain, the file is never read.
		config.WithCredentials(credentials.NewCredentials(&credentials.SharedCredentialsProvider{
			Filename: filename,
			Profile:  profile,
		}))
	}

	config = request.WithRetryer(config, log.NewRetryLogger(DefaultRetryer))
	sess, err := session.NewSession(config)
	if err != nil {
		return nil, err
	}

	return sess, nil
}

func newELBv2Client(region string, cacheCfg *cache.Config) elbv2iface.ELBV2API {
	sess, err := newAWSSession(region)
	if err != nil {
		log.Fatalf("failed to create AWS session, %s", err)
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
	sess, err := newAWSSession(region)
	if err != nil {
		log.Fatalf("failed to create AWS session, %s", err)
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
	sess, err := newAWSSession(region)
	if err != nil {
		log.Fatalf("failed to create AWS session, %s", err)
	}

	return sqs.New(sess)
}

func newASGClient(region string) autoscalingiface.AutoScalingAPI {
	sess, err := newAWSSession(region)
	if err != nil {
		log.Fatalf("failed to create AWS session, %s", err)
	}

	return autoscaling.New(sess)
}
