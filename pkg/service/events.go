package service

import (
	"fmt"
	"time"

	"github.com/keikoproj/lifecycle-manager/pkg/log"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

type EventReason string
type EventLevel string

var (
	// EventName is the default name for service events
	EventName = "lifecycle-manager.%v"
	// EventNamespace is the default namespace in which events will be published in
	EventNamespace = "default"

	EventLevelNormal  = "Normal"
	EventLevelWarning = "Warning"

	EventReasonLifecycleHookReceived  EventReason = "EventLifecycleHookReceived"
	EventMessageLifecycleHookReceived             = "lifecycle hook for event %v was received, instance %v will begin processing"

	EventReasonLifecycleHookProcessed  EventReason = "EventLifecycleHookProcessed"
	EventMessageLifecycleHookProcessed             = "lifecycle hook for event %v has completed processing, instance %v gracefully terminated"

	EventReasonLifecycleHookFailed  EventReason = "EventLifecycleHookFailed"
	EventMessageLifecycleHookFailed             = "lifecycle hook for event %v has failed processing: %v"

	EventReasonNodeDrainSucceeded  EventReason = "EventReasonNodeDrainSucceeded"
	EventMessageNodeDrainSucceeded             = "node %v has been drained successfully as a response to a termination event"

	EventReasonNodeDrainFailed  EventReason = "EventReasonNodeDrainFailed"
	EventMessageNodeDrainFailed             = "node %v draining has failed: %v"

	EventReasonTargetDeregisterSucceeded  EventReason = "EventReasonTargetDeregisterSucceeded"
	EventMessageTargetDeregisterSucceeded             = "target %v:%v has successfully deregistered from target group %v"

	EventReasonTargetDeregisterFailed  EventReason = "EventReasonTargetDeregisterFailed"
	EventMessageTargetDeregisterFailed             = "target %v:%v has failed to deregistered from target group %v: %v"

	EventReasonInstanceDeregisterSucceeded  EventReason = "EventReasonInstanceDeregisterSucceeded"
	EventMessageInstanceDeregisterSucceeded             = "instance %v has successfully deregistered from classic-elb %v"

	EventReasonInstanceDeregisterFailed  EventReason = "EventReasonInstanceDeregisterFailed"
	EventMessageInstanceDeregisterFailed             = "instance %v has failed to deregistered from classic-elb %v: %v"

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

func newKubernetesEvent(reason EventReason, message string, refNodeName string) *v1.Event {
	var objReference v1.ObjectReference
	if refNodeName != "" {
		objReference = v1.ObjectReference{Kind: "Node", Name: refNodeName}
	}
	event := &v1.Event{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf(EventName, time.Now().UnixNano()),
			Namespace: EventNamespace,
		},
		Reason:  string(reason),
		Message: string(message),
		Type:    getReasonEventLevel(reason),
		LastTimestamp: metav1.Time{
			Time: time.Now(),
		},
		InvolvedObject: objReference,
	}
	return event
}
