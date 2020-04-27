package service

import (
	"time"

	"github.com/aws/aws-sdk-go/service/sqs"
	"github.com/keikoproj/lifecycle-manager/pkg/log"
	v1 "k8s.io/api/core/v1"
)

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
	message              *sqs.Message
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

// SetMessage is a setter method for the sqs message body
func (e *LifecycleEvent) SetMessage(message *sqs.Message) { e.message = message }

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
