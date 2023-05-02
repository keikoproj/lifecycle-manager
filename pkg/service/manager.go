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
	workQueue       []*LifecycleEvent
	targets         *sync.Map
	metrics         *MetricsServer
	avarageLatency  float64
	completedEvents int
	rejectedEvents  int
	failedEvents    int
}

// ManagerContext contain the user input parameters on the current context
type ManagerContext struct {
	CacheConfig                *cache.Config
	KubectlLocalPath           string
	QueueName                  string
	Region                     string
	DrainTimeoutUnknownSeconds int64
	DrainTimeoutSeconds        int64
	DrainRetryIntervalSeconds  int64
	//DrainRetryAttempts         int64
	PollingIntervalSeconds  int64
	WithDeregister          bool
	MaxDrainConcurrency     *semaphore.Weighted
	MaxTimeToProcessSeconds int64
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
		eventStream:   make(chan *sqs.Message, 0),
		workQueue:     make([]*LifecycleEvent, 0),
		metrics:       &MetricsServer{},
		targets:       &sync.Map{},
		authenticator: auth,
		context:       ctx,
	}
}

func (mgr *Manager) AddEvent(event *LifecycleEvent) {
	var (
		metrics = mgr.metrics
		kube    = mgr.authenticator.KubernetesClient
	)
	mgr.Lock()
	event.SetEventTimeStarted(time.Now())
	metrics.IncGauge(TerminatingInstancesCountMetric)

	if !mgr.EventInQueue(event) {
		mgr.workQueue = append(mgr.workQueue, event)
	}

	mgr.Unlock()

	msg := fmt.Sprintf(EventMessageLifecycleHookReceived, event.RequestID, event.EC2InstanceID)
	kEvent := newKubernetesEvent(EventReasonLifecycleHookReceived, getMessageFields(event, msg))
	publishKubernetesEvent(kube, kEvent)
}

func (mgr *Manager) EventInQueue(e *LifecycleEvent) bool {
	for _, event := range mgr.workQueue {
		if event.RequestID == e.RequestID {
			return true
		}
	}
	return false
}

func (mgr *Manager) RemoveFromQueue(event *LifecycleEvent) {
	for idx, ev := range mgr.workQueue {
		if event.RequestID == ev.RequestID {
			mgr.Lock()
			mgr.workQueue = append(mgr.workQueue[0:idx], mgr.workQueue[idx+1:]...)
			mgr.Unlock()
		}
	}
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

	mgr.completedEvents++

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

	mgr.RemoveFromQueue(event)
	msg := fmt.Sprintf(EventMessageLifecycleHookProcessed, event.RequestID, event.EC2InstanceID, t)
	kEvent := newKubernetesEvent(EventReasonLifecycleHookProcessed, getMessageFields(event, msg))
	publishKubernetesEvent(kubeClient, kEvent)

	metrics.AddCounter(SuccessfulEventsTotalMetric, 1)
	metrics.DecGauge(TerminatingInstancesCountMetric)
	metrics.SetGauge(AverageDurationSecondsMetric, mgr.avarageLatency)
	log.Infof("event %v for instance %v completed after %vs", event.RequestID, event.EC2InstanceID, t)
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
	kEvent := newKubernetesEvent(EventReasonLifecycleHookFailed, getMessageFields(event, msg))
	publishKubernetesEvent(kubeClient, kEvent)

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
