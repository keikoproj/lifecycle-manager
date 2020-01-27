package service

import (
	"encoding/json"
	"fmt"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func Test_NewEvent(t *testing.T) {
	t.Log("Test_NewEvent: should be able to get a new kubernetes event")
	msg := fmt.Sprintf(EventMessageInstanceDeregisterFailed, "i-123456789012", "my-load-balancer", "some bad error occurred")
	msgFields := map[string]string{
		"ec2InstanceId": "i-123456789012",
		"loadBalancer":  "my-load-balancer",
		"error":         "some bad error occurred",
		"details":       msg,
	}
	event := newKubernetesEvent(EventReasonInstanceDeregisterFailed, msgFields)

	if event.Reason != string(EventReasonInstanceDeregisterFailed) {
		t.Fatalf("expected event.Reason to be: %v, got: %v", string(EventReasonInstanceDeregisterFailed), event.Reason)
	}

	// decode the JSON
	var msgPayload map[string]string
	err := json.Unmarshal([]byte(event.Message), &msgPayload)
	if err != nil {
		t.Fatalf("json.Unmarshal Failed, %s", err)
	}

	if msg != msgPayload["details"] {
		t.Fatalf("expected event.Message to be: %v, got: %v", msg, msgPayload["details"])
	}

}

func Test_PublishEvent(t *testing.T) {
	t.Log("Test_PublishEvent: should be able to publish kubernetes events")
	kubeClient := fake.NewSimpleClientset()
	msg := fmt.Sprintf(EventMessageInstanceDeregisterFailed, "i-123456789012", "my-load-balancer", "some bad error occurred")
	msgFields := map[string]string{
		"ec2InstanceId": "i-123456789012",
		"loadBalancer":  "my-load-balancer",
		"error":         "some bad error occurred",
		"details":       msg,
	}
	event := newKubernetesEvent(EventReasonInstanceDeregisterFailed, msgFields)

	publishKubernetesEvent(kubeClient, event)
	expectedEvents := 1

	events, err := kubeClient.CoreV1().Events(EventNamespace).List(metav1.ListOptions{})
	if err != nil {
		t.Fatalf("Test_PublishEvent: expected error not to have occured, %v", err)
	}

	if len(events.Items) != expectedEvents {
		t.Fatalf("Test_PublishEvent: expected %v events, found: %v", expectedEvents, len(events.Items))
	}

	// decode the JSON
	var msgPayload map[string]string
	err = json.Unmarshal([]byte(event.Message), &msgPayload)
	if err != nil {
		t.Fatalf("json.Unmarshal Failed, %s", err)
	}
	if msgPayload["ec2InstanceId"] != "i-123456789012" {
		t.Fatalf("Expected=%s, Got=%s", "i-123456789012", msgPayload["ec2InstanceId"])
	}
	if msg != msgPayload["details"] {
		t.Fatalf("expected event.Message to be: %v, got: %v", msg, msgPayload["details"])
	}

}
