package service

import (
	"sync"
	"time"

	"github.com/aws/aws-sdk-go/service/autoscaling/autoscalingiface"
	"github.com/aws/aws-sdk-go/service/elb/elbiface"
	"github.com/aws/aws-sdk-go/service/elbv2/elbv2iface"
	"github.com/aws/aws-sdk-go/service/sqs"
	"github.com/aws/aws-sdk-go/service/sqs/sqsiface"
	"github.com/keikoproj/lifecycle-manager/pkg/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/ticketmaster/aws-sdk-go-cache/cache"
	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
)

type Authenticator struct {
	ScalingGroupClient autoscalingiface.AutoScalingAPI
	SQSClient          sqsiface.SQSAPI
	ELBv2Client        elbv2iface.ELBV2API
	ELBClient          elbiface.ELBAPI
	KubernetesClient   kubernetes.Interface
}

type Manager struct {
	eventStream     chan *sqs.Message
	authenticator   Authenticator
	context         ManagerContext
	workQueue       []*LifecycleEvent
	workQueueSync   *sync.Mutex
	metrics         *MetricsServer
	avarageLatency  float64
	completedEvents int
	rejectedEvents  int
	failedEvents    int
}

type MetricsServer struct {
	Counters map[string]prometheus.Counter
	Gauges   map[string]prometheus.Gauge
}

func (m *MetricsServer) AddCounter(idx string, value float64) {
	if val, ok := m.Counters[idx]; ok {
		val.Add(value)
	}
}

func (m *MetricsServer) SetGauge(idx string, value float64) {
	if val, ok := m.Gauges[idx]; ok {
		val.Set(value)
	}
}

func (m *MetricsServer) IncGauge(idx string) {
	if val, ok := m.Gauges[idx]; ok {
		val.Inc()
	}
}

func (m *MetricsServer) DecGauge(idx string) {
	if val, ok := m.Gauges[idx]; ok {
		val.Dec()
	}
}

func New(auth Authenticator, ctx ManagerContext) *Manager {
	return &Manager{
		eventStream:   make(chan *sqs.Message, 0),
		workQueue:     make([]*LifecycleEvent, 0),
		metrics:       &MetricsServer{},
		workQueueSync: &sync.Mutex{},
		authenticator: auth,
		context:       ctx,
	}
}

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

type LifecycleEvent struct {
	LifecycleHookName    string `json:"LifecycleHookName"`
	AccountID            string `json:"AccountId"`
	RequestID            string `json:"RequestId"`
	LifecycleTransition  string `json:"LifecycleTransition"`
	AutoScalingGroupName string `json:"AutoScalingGroupName"`
	EC2InstanceID        string `json:"EC2InstanceId"`
	LifecycleActionToken string `json:"LifecycleActionToken"`
	receiptHandle        string
	queueURL             string
	heartbeatInterval    int64
	referencedNode       v1.Node
	drainCompleted       bool
	deregisterCompleted  bool
	eventCompleted       bool
	startTime            time.Time
}

func (e *LifecycleEvent) IsValid() bool {
	if e.LifecycleTransition != TerminationEventName {
		log.Warnf("got unsupported event type: '%+v'", e.LifecycleTransition)
		return false
	}

	if e.EC2InstanceID == "" {
		log.Warnf("instance-id not provided in event: %+v", e)
		return false
	}

	if e.LifecycleHookName == "" {
		log.Warnf("hook-name not provided in event: %+v", e)
		return false
	}

	return true
}

func (e *LifecycleEvent) IsAlreadyExist(queue []*LifecycleEvent) bool {
	for _, event := range queue {
		if event.RequestID == e.RequestID {
			log.Debugf("event %v already being processed, discarding", event.RequestID)
			return true
		}
	}
	return false
}

// SetReceiptHandle is a setter method for the receipt handle of the event
func (e *LifecycleEvent) SetReceiptHandle(receipt string) { e.receiptHandle = receipt }

// SetQueueURL is a setter method for the url of the SQS queue
func (e *LifecycleEvent) SetQueueURL(url string) { e.queueURL = url }

// SetHeartbeatInterval is a setter method for heartbeat interval of the event
func (e *LifecycleEvent) SetHeartbeatInterval(interval int64) { e.heartbeatInterval = interval }

// SetReferencedNode is a setter method for the event referenced node
func (e *LifecycleEvent) SetReferencedNode(node v1.Node) { e.referencedNode = node }

// SetDrainCompleted is a setter method for status of the drain operation
func (e *LifecycleEvent) SetDrainCompleted(val bool) { e.drainCompleted = val }

// SetDeregisterCompleted is a setter method for status of the drain operation
func (e *LifecycleEvent) SetDeregisterCompleted(val bool) { e.deregisterCompleted = val }

// SetEventCompleted is a setter method for status of the drain operation
func (e *LifecycleEvent) SetEventCompleted(val bool) { e.eventCompleted = val }

// SetEventTimeStarted is a setter method for the time an event started
func (e *LifecycleEvent) SetEventTimeStarted(t time.Time) { e.startTime = t }
