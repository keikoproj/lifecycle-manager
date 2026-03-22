package service

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/sqs"
)

func Test_Metrics(t *testing.T) {
	t.Log("Test_Metrics: should be able to start metrics server")
	var (
		fakeQueueName   = "my-queue"
		fakeMessageBody = "message-body"
	)
	sqsStubber := &stubSQS{
		FakeQueueName: fakeQueueName,
		FakeQueueMessages: []*sqs.Message{
			{
				Body: aws.String(fakeMessageBody),
			},
		},
	}

	auth := Authenticator{
		SQSClient: sqsStubber,
	}

	ctx := ManagerContext{
		QueueName:              "my-queue",
		Region:                 "us-west-2",
		PollingIntervalSeconds: 10,
	}

	mgr := New(auth, ctx)

	// Use port :0 so the OS assigns a free port, avoiding conflicts with other
	// services or parallel test runs. Read the actual bound address via the
	// Addr channel before making the HTTP request.
	savedPort := MetricsPort
	MetricsPort = ":0"
	defer func() { MetricsPort = savedPort }()

	mgr.metrics.Addr = make(chan string, 1)
	go mgr.metrics.Start()
	addr := <-mgr.metrics.Addr

	endpoint := fmt.Sprintf("http://%v%v", addr, MetricsEndpoint)
	resp, err := http.Get(endpoint)
	if err != nil {
		t.Fatalf("handleEvent: expected error not to have occured, %v", err)
	}

	expectedStatusCode := 200
	if resp.StatusCode != expectedStatusCode {
		t.Fatalf("expected status code: %v, got: %v", expectedStatusCode, resp.StatusCode)
	}
}
