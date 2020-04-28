package service

import (
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/sqs"
	"github.com/aws/aws-sdk-go/service/sqs/sqsiface"
)

type stubSQS struct {
	sqsiface.SQSAPI
	FakeQueueMessages         []*sqs.Message
	FakeQueueName             string
	timesCalledReceiveMessage int
	timesCalledDeleteMessage  int
	timesCalledGetQueueUrl    int
}

func (s *stubSQS) ReceiveMessage(input *sqs.ReceiveMessageInput) (*sqs.ReceiveMessageOutput, error) {
	s.timesCalledReceiveMessage++
	if len(s.FakeQueueMessages) != 0 {
		return &sqs.ReceiveMessageOutput{Messages: s.FakeQueueMessages}, nil
	}
	return &sqs.ReceiveMessageOutput{}, nil
}

func (s *stubSQS) DeleteMessage(input *sqs.DeleteMessageInput) (*sqs.DeleteMessageOutput, error) {
	s.timesCalledDeleteMessage++
	return &sqs.DeleteMessageOutput{}, nil
}

func (a *stubSQS) GetQueueUrl(input *sqs.GetQueueUrlInput) (*sqs.GetQueueUrlOutput, error) {
	a.timesCalledGetQueueUrl++
	queueURL := fmt.Sprintf("https://queue.amazonaws.com/80398EXAMPLE/%v", a.FakeQueueName)

	if aws.StringValue(input.QueueName) == a.FakeQueueName {
		return &sqs.GetQueueUrlOutput{QueueUrl: aws.String(queueURL)}, nil
	}
	return &sqs.GetQueueUrlOutput{}, nil
}

func _quitPollerAfter(quitter chan bool, seconds int64) {
	time.Sleep(time.Duration(seconds)*time.Second + time.Duration(500)*time.Millisecond)
	quitter <- true
}

func Test_DesserializeMessage(t *testing.T) {
	t.Log("Test_DesserializeMessage: should be able to deserialize a string to sqs Message")
	message := `{"Attributes":{"SenderId":"ABCD:efg"},"Body":"{\"LifecycleHookName\":\"test\",\"AccountId\":\"0000000\",\"RequestId\":\"c2281dfd\",\"LifecycleTransition\":\"autoscaling:EC2_INSTANCE_TERMINATING\",\"AutoScalingGroupName\":\"some-asg\",\"Service\":\"AWS Auto Scaling\",\"Time\":\"2020-01-15T03:54:51.913Z\",\"EC2InstanceId\":\"i-0000000000\",\"LifecycleActionToken\":\"c7b2144c\"}","MD5OfBody":"123123123123","MD5OfMessageAttributes":null,"MessageAttributes":null,"MessageId":"c643f9fc","ReceiptHandle":"AQEBVkU="}`
	sqsMessage, err := deserializeMessage(message)
	if err != nil {
		t.Fatalf("deserializeMessage: expected error not to have occured, %v", err)
	}
	if aws.StringValue(sqsMessage.Body) == "" {
		t.Fatalf("deserializeMessage: expected sqsMessage.Body not to be empty")
	}
}

func Test_GetQueueURLByNamePositive(t *testing.T) {
	t.Log("Test_GetQueueURLByName: should be able to fetch queue URL by it's name")
	fakeQueueName := "my-queue"
	stubber := &stubSQS{
		FakeQueueName: fakeQueueName,
	}
	url := getQueueURLByName(stubber, fakeQueueName)
	expectedURL := fmt.Sprintf("https://queue.amazonaws.com/80398EXAMPLE/%v", fakeQueueName)
	expectedTimesCalled := 1
	if url != expectedURL {
		t.Fatalf("expected getQueueURLByName: %v, got: %v", expectedURL, url)
	}

	if stubber.timesCalledGetQueueUrl != expectedTimesCalled {
		t.Fatalf("expected timesCalledGetQueueUrl: %v, got: %v", expectedTimesCalled, stubber.timesCalledGetQueueUrl)
	}
}

func Test_GetQueueURLByNameNegative(t *testing.T) {
	t.Log("Test_GetQueueURLByNameNegative: should return empty string if failed to find URL")
	fakeQueueName := "my-queue"
	stubber := &stubSQS{
		FakeQueueName: "other-queue",
	}
	url := getQueueURLByName(stubber, fakeQueueName)
	expectedURL := ""
	expectedTimesCalled := 1
	if url != expectedURL {
		t.Fatalf("expected getQueueURLByName: %v, got: %v", expectedURL, url)
	}

	if stubber.timesCalledGetQueueUrl != expectedTimesCalled {
		t.Fatalf("expected timesCalledGetQueueUrl: %v, got: %v", expectedTimesCalled, stubber.timesCalledGetQueueUrl)
	}
}

func Test_DeleteMessage(t *testing.T) {
	t.Log("Test_DeleteMessage: should delete a message from queue")
	fakeQueueName := "my-queue"
	fakeReceiptHandle := "MbZj6wDWli+JvwwJaBV+3dcjk2YW2vA3+STFFljTM8tJJg6HRG6PYSasuWXPJB+Cw="
	stubber := &stubSQS{
		FakeQueueName: fakeQueueName,
	}
	url := getQueueURLByName(stubber, fakeQueueName)
	err := deleteMessage(stubber, url, fakeReceiptHandle)
	if err != nil {
		t.Fatalf("deleteMessage: expected error not to have occured, %v", err)
	}
	expectedTimesCalled := 1

	if stubber.timesCalledDeleteMessage != expectedTimesCalled {
		t.Fatalf("expected timesCalledDeleteMessage: %v, got: %v", expectedTimesCalled, stubber.timesCalledDeleteMessage)
	}
}

func Test_ReadMessage(t *testing.T) {
	t.Log("Test_ReadMessage: should unmarshal a message into a LifecycleEvent")
	expectedLifecycleEvent := &LifecycleEvent{
		LifecycleHookName:    "my-hook",
		AccountID:            "12345689012",
		RequestID:            "63f5b5c2-58b3-0574-b7d5-b3162d0268f0",
		LifecycleTransition:  "autoscaling:EC2_INSTANCE_TERMINATING",
		AutoScalingGroupName: "my-asg",
		EC2InstanceID:        "i-123486890234",
		LifecycleActionToken: "cc34960c-1e41-4703-a665-bdb3e5b81ad3",
		receiptHandle:        "MbZj6wDWli+JvwwJaBV+3dcjk2YW2vA3+STFFljTM8tJJg6HRG6PYSasuWXPJB+Cw=",
		queueURL:             "some-queue",
	}
	fakeMessage := &sqs.Message{
		Body:          aws.String(`{"LifecycleHookName":"my-hook","AccountId":"12345689012","RequestId":"63f5b5c2-58b3-0574-b7d5-b3162d0268f0","LifecycleTransition":"autoscaling:EC2_INSTANCE_TERMINATING","AutoScalingGroupName":"my-asg","Service":"AWS Auto Scaling","Time":"2019-09-27T02:39:14.183Z","EC2InstanceId":"i-123486890234","LifecycleActionToken":"cc34960c-1e41-4703-a665-bdb3e5b81ad3"}`),
		ReceiptHandle: aws.String("MbZj6wDWli+JvwwJaBV+3dcjk2YW2vA3+STFFljTM8tJJg6HRG6PYSasuWXPJB+Cw="),
	}
	expectedLifecycleEvent.SetMessage(fakeMessage)

	event, err := readMessage(fakeMessage, "some-queue")
	if err != nil {
		t.Fatalf("readMessage: expected error not to have occured, %v", err)
	}

	if !reflect.DeepEqual(expectedLifecycleEvent, event) {
		t.Fatalf("readMessage: expected event: %+v got: %+v", expectedLifecycleEvent, event)
	}
}
