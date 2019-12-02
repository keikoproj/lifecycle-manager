package service

import (
	"fmt"
	"net/http"
	"testing"
	"time"

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

	go mgr.metrics.Start()
	time.Sleep(2 * time.Second)

	endpoint := fmt.Sprintf("http://127.0.0.1%v%v", MetricsPort, MetricsEndpoint)
	resp, err := http.Get(endpoint)
	if err != nil {
		t.Fatalf("handleEvent: expected error not to have occured, %v", err)
	}

	expectedStatusCode := 200
	if resp.StatusCode != expectedStatusCode {
		t.Fatalf("expected status code: %v, got: %v", expectedStatusCode, resp.StatusCode)
	}
}
