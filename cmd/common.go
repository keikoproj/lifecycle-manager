package cmd

import (
	"github.com/aws/aws-sdk-go/aws"
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

func newIAMClient(region string) iamiface.IAMAPI {
	config := aws.NewConfig().WithRegion(region)
	config = config.WithCredentialsChainVerboseErrors(true)
	sess, err := session.NewSession(config)
	if err != nil {
		log.Fatalf("failed to create iam client, %v", err)
	}
	return iam.New(sess)
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
