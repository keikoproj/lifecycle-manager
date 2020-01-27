package service

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"time"

	"github.com/keikoproj/lifecycle-manager/pkg/log"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// EventReason defines the reason of an event
type EventReason string

// EventLevel defines the level of an event
type EventLevel string

const (
	// EventLevelNormal is the level of a normal event
	EventLevelNormal = "Normal"
	// EventLevelWarning is the level of a warning event
	EventLevelWarning = "Warning"
	// EventReasonLifecycleHookReceived is the reason for a lifecycle received event
	EventReasonLifecycleHookReceived EventReason = "LifecycleHookReceived"
	// EventMessageLifecycleHookReceived is the message for a lifecycle received event
	EventMessageLifecycleHookReceived = "lifecycle hook for event %v was received, instance %v will begin processing"
	// EventReasonLifecycleHookProcessed is the reason for a lifecycle successful processing event
	EventReasonLifecycleHookProcessed EventReason = "LifecycleHookProcessed"
	//EventMessageLifecycleHookProcessed is the message for a lifecycle successful processing event
	EventMessageLifecycleHookProcessed = "lifecycle hook for event %v has completed processing, instance %v gracefully terminated after %vs"
	// EventReasonLifecycleHookFailed is the reason for a lifecycle failed event
	EventReasonLifecycleHookFailed EventReason = "LifecycleHookFailed"
	// EventMessageLifecycleHookFailed is the message for a lifecycle failed event
	EventMessageLifecycleHookFailed = "lifecycle hook for event %v has failed processing after %vs: %v"
	// EventReasonNodeDrainSucceeded is the reason for a successful drain event
	EventReasonNodeDrainSucceeded EventReason = "NodeDrainSucceeded"
	// EventMessageNodeDrainSucceeded is the message for a successful drain event
	EventMessageNodeDrainSucceeded = "node %v has been drained successfully as a response to a termination event"
	// EventReasonNodeDrainFailed is the reason for a failed drain event
	EventReasonNodeDrainFailed EventReason = "NodeDrainFailed"
	// EventMessageNodeDrainFailed is the message for a failed drain event
	EventMessageNodeDrainFailed = "node %v draining has failed: %v"
	// EventReasonTargetDeregisterSucceeded is the reason for a successful target group deregister event
	EventReasonTargetDeregisterSucceeded EventReason = "TargetDeregisterSucceeded"
	// EventMessageTargetDeregisterSucceeded is the message for a successful target group deregister event
	EventMessageTargetDeregisterSucceeded = "target %v:%v has successfully deregistered from target group %v"
	// EventReasonTargetDeregisterFailed is the reason for a successful drain event
	EventReasonTargetDeregisterFailed EventReason = "TargetDeregisterFailed"
	// EventMessageTargetDeregisterFailed is the message for a successful drain event
	EventMessageTargetDeregisterFailed = "target %v has failed to deregistered from target group %v: %v"
	// EventReasonInstanceDeregisterSucceeded is the reason for a successful target group deregister event
	EventReasonInstanceDeregisterSucceeded EventReason = "InstanceDeregisterSucceeded"
	// EventMessageInstanceDeregisterSucceeded is the message for a successful target group deregister event
	EventMessageInstanceDeregisterSucceeded = "instance %v has successfully deregistered from classic-elb %v"
	// EventReasonInstanceDeregisterFailed is the reason for a successful classic elb deregister event
	EventReasonInstanceDeregisterFailed EventReason = "InstanceDeregisterFailed"
	// EventMessageInstanceDeregisterFailed is the message for a successful classic elb deregister event
	EventMessageInstanceDeregisterFailed = "instance %v has failed to deregister from classic-elb %v: %v"
)

var (
	// EventName is the default name for service events
	EventName = "lifecycle-manager.%v"
	// EventNamespace is the default namespace in which events will be published in
	EventNamespace = "default"

	// EventLevels is a map of event reasons and their event level
	EventLevels = map[EventReason]string{
		EventReasonLifecycleHookReceived:       EventLevelNormal,
		EventReasonLifecycleHookProcessed:      EventLevelNormal,
		EventReasonLifecycleHookFailed:         EventLevelWarning,
		EventReasonNodeDrainSucceeded:          EventLevelNormal,
		EventReasonNodeDrainFailed:             EventLevelWarning,
		EventReasonTargetDeregisterSucceeded:   EventLevelNormal,
		EventReasonTargetDeregisterFailed:      EventLevelWarning,
		EventReasonInstanceDeregisterSucceeded: EventLevelNormal,
		EventReasonInstanceDeregisterFailed:    EventLevelWarning,
	}
)

func publishKubernetesEvent(kubeClient kubernetes.Interface, event *v1.Event) {
	log.Debugf("publishing event: %v", event.Reason)
	_, err := kubeClient.CoreV1().Events(EventNamespace).Create(event)
	if err != nil {
		log.Errorf("failed to publish event: %v", err)
	}
}

func getReasonEventLevel(reason EventReason) string {
	if val, ok := EventLevels[reason]; ok {
		return val
	}
	return "Normal"
}

func newKubernetesEvent(reason EventReason, msgFields map[string]string) *v1.Event {
	// Marshal as JSON
	b, err := json.Marshal(msgFields)
	msgPayload := string(b)
	// I think it is very tough to trigger this error since json.Marshal function can return two types of errors
	// UnsupportedTypeError or UnsupportedValueError. Since our type is very rigid, these errors won't be triggered.
	if err != nil {
		log.Errorf("json.Marshal Failed, %s", err)
		// let's convert map to string since encoding as JSON is failing. At the least the information will be conveyed
		msgPayload = fmt.Sprintf("%v", msgFields)
	}

	eventName := fmt.Sprintf("%v-%v.%v", "lifecycle-manager", time.Now().Unix(), rand.Int())
	event := &v1.Event{
		ObjectMeta: metav1.ObjectMeta{
			Name:      eventName,
			Namespace: EventNamespace,
		},
		Reason:  string(reason),
		Message: msgPayload,
		Type:    getReasonEventLevel(reason),
		LastTimestamp: metav1.Time{
			Time: time.Now(),
		},
	}
	return event
}
