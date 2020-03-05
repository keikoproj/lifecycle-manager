package service

import (
	"fmt"
	"reflect"
	"sync"
	"time"

	"golang.org/x/sync/semaphore"

	"github.com/aws/aws-sdk-go/service/autoscaling/autoscalingiface"
	"github.com/aws/aws-sdk-go/service/elb/elbiface"
	"github.com/aws/aws-sdk-go/service/elbv2/elbv2iface"
	"github.com/aws/aws-sdk-go/service/sqs"
	"github.com/aws/aws-sdk-go/service/sqs/sqsiface"
	"github.com/keikoproj/lifecycle-manager/pkg/log"

	"github.com/keikoproj/aws-sdk-go-cache/cache"

	"k8s.io/client-go/kubernetes"
)

// Manager is the main object for lifecycle-manager and holds the state
type Manager struct {
	eventStream      chan *sqs.Message
	authenticator    Authenticator
	context          ManagerContext
	deregistrationMu sync.Mutex
	sync.Mutex
	workQueue           []*LifecycleEvent
	targets             *sync.Map
	metrics             *MetricsServer
	maxDrainConcurrency *semaphore.Weighted
	avarageLatency      float64
	completedEvents     int
	rejectedEvents      int
	failedEvents        int
}

// ManagerContext contain the user input parameters on the current context
type ManagerContext struct {
	CacheConfig               *cache.Config
	KubectlLocalPath          string
	QueueName                 string
	Region                    string
	DrainTimeoutSeconds       int64
	DrainRetryIntervalSeconds int64
	PollingIntervalSeconds    int64
	WithDeregister            bool
}

// Authenticator holds clients for all required APIs
type Authenticator struct {
	ScalingGroupClient autoscalingiface.AutoScalingAPI
	SQSClient          sqsiface.SQSAPI
	ELBv2Client        elbv2iface.ELBV2API
	ELBClient          elbiface.ELBAPI
	KubernetesClient   kubernetes.Interface
}

// ScanResult contains a list of found load balancers and target groups
type ScanResult struct {
	ActiveLoadBalancers []string
	ActiveTargetGroups  map[string]int64
}

type Waiter struct {
	sync.WaitGroup
	finished               chan bool
	errors                 chan WaiterError
	classicWaiterCount     int
	targetGroupWaiterCount int
}

func (w *Waiter) IncClassicWaiter()     { w.classicWaiterCount++ }
func (w *Waiter) DecClassicWaiter()     { w.classicWaiterCount-- }
func (w *Waiter) IncTargetGroupWaiter() { w.targetGroupWaiterCount++ }
func (w *Waiter) DecTargetGroupWaiter() { w.targetGroupWaiterCount-- }

type WaiterError struct {
	Error error
	Type  TargetType
}

func New(auth Authenticator, ctx ManagerContext) *Manager {
	return &Manager{
		eventStream:         make(chan *sqs.Message, 0),
		workQueue:           make([]*LifecycleEvent, 0),
		maxDrainConcurrency: semaphore.NewWeighted(MaxDrainConcurrency),
		metrics:             &MetricsServer{},
		targets:             &sync.Map{},
		authenticator:       auth,
		context:             ctx,
	}
}

func (mgr *Manager) AddEvent(event *LifecycleEvent) {
	var (
		metrics = mgr.metrics
	)
	mgr.Lock()
	event.SetEventTimeStarted(time.Now())
	metrics.IncGauge(TerminatingInstancesCountMetric)
	mgr.workQueue = append(mgr.workQueue, event)
	mgr.Unlock()
}

func (mgr *Manager) CompleteEvent(event *LifecycleEvent) {
	var (
		queue      = mgr.authenticator.SQSClient
		metrics    = mgr.metrics
		kubeClient = mgr.authenticator.KubernetesClient
		asgClient  = mgr.authenticator.ScalingGroupClient
		url        = event.queueURL
		t          = time.Since(event.startTime).Seconds()
	)

	if mgr.avarageLatency == 0 {
		mgr.avarageLatency = t
	} else {
		mgr.avarageLatency = (mgr.avarageLatency + t) / 2
	}

	newQueue := make([]*LifecycleEvent, 0)
	for _, e := range mgr.workQueue {
		if reflect.DeepEqual(event, e) {
			// found event in work queue
			log.Infof("event %v completed processing", event.RequestID)
			event.SetEventCompleted(true)

			err := deleteMessage(queue, url, event.receiptHandle)
			if err != nil {
				log.Errorf("failed to delete message: %v", err)
			}
			err = completeLifecycleAction(asgClient, *event, ContinueAction)
			if err != nil {
				log.Errorf("failed to complete lifecycle action: %v", err)
			}
			msg := fmt.Sprintf(EventMessageLifecycleHookProcessed, event.RequestID, event.EC2InstanceID, t)
			msgFields := map[string]string{
				"eventID":       event.RequestID,
				"ec2InstanceId": event.EC2InstanceID,
				"asgName":       event.AutoScalingGroupName,
				"details":       msg,
			}
			publishKubernetesEvent(kubeClient, newKubernetesEvent(EventReasonLifecycleHookProcessed, msgFields))
			metrics.AddCounter(SuccessfulEventsTotalMetric, 1)
		} else {
			newQueue = append(newQueue, e)
		}
	}
	mgr.Lock()
	mgr.workQueue = newQueue
	mgr.completedEvents++
	metrics.DecGauge(TerminatingInstancesCountMetric)
	metrics.SetGauge(AverageDurationSecondsMetric, mgr.avarageLatency)
	log.Infof("event %v for instance %v completed after %vs", event.RequestID, event.EC2InstanceID, t)
	mgr.Unlock()
}

func (mgr *Manager) FailEvent(err error, event *LifecycleEvent, abandon bool) {
	var (
		auth               = mgr.authenticator
		kubeClient         = auth.KubernetesClient
		queue              = auth.SQSClient
		metrics            = mgr.metrics
		scalingGroupClient = auth.ScalingGroupClient
		url                = event.queueURL
		t                  = time.Since(event.startTime).Seconds()
	)
	log.Errorf("event %v has failed processing after %vs: %v", event.RequestID, t, err)
	mgr.failedEvents++
	metrics.AddCounter(FailedEventsTotalMetric, 1)
	event.SetEventCompleted(true)
	msg := fmt.Sprintf(EventMessageLifecycleHookFailed, event.RequestID, t, err)
	msgFields := map[string]string{
		"eventID":       event.RequestID,
		"ec2InstanceId": event.EC2InstanceID,
		"asgName":       event.AutoScalingGroupName,
		"details":       msg,
	}
	publishKubernetesEvent(kubeClient, newKubernetesEvent(EventReasonLifecycleHookFailed, msgFields))

	if abandon {
		log.Warnf("abandoning instance %v", event.EC2InstanceID)
		err := completeLifecycleAction(scalingGroupClient, *event, AbandonAction)
		if err != nil {
			log.Errorf("completeLifecycleAction Failed, %s", err)
		}
	}

	if reflect.DeepEqual(event, LifecycleEvent{}) {
		log.Errorf("event failed: invalid message: %v", err)
		return
	}

	err = deleteMessage(queue, url, event.receiptHandle)
	if err != nil {
		log.Errorf("event failed: failed to delete message: %v", err)
	}

}

func (mgr *Manager) RejectEvent(err error, event *LifecycleEvent) {
	var (
		metrics = mgr.metrics
		auth    = mgr.authenticator
		queue   = auth.SQSClient
		url     = event.queueURL
	)

	log.Debugf("event %v has been rejected for processing: %v", event.RequestID, err)
	mgr.rejectedEvents++
	metrics.AddCounter(RejectedEventsTotalMetric, 1)

	if reflect.DeepEqual(event, LifecycleEvent{}) {
		log.Errorf("event failed: invalid message: %v", err)
		return
	}

	err = deleteMessage(queue, url, event.receiptHandle)
	if err != nil {
		log.Errorf("failed to delete message: %v", err)
	}
}
