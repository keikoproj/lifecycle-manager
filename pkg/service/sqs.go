package service

import (
	"encoding/json"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/sqs"
	"github.com/aws/aws-sdk-go/service/sqs/sqsiface"
	"github.com/keikoproj/lifecycle-manager/pkg/log"
)

func getQueueURLByName(s sqsiface.SQSAPI, name string) string {
	resultURL, err := s.GetQueueUrl(&sqs.GetQueueUrlInput{
		QueueName: aws.String(name),
	})
	if err != nil {
		if aerr, ok := err.(awserr.Error); ok && aerr.Code() == sqs.ErrCodeQueueDoesNotExist {
			log.Fatalf("unable to find queue %v: %v", name, aerr)
		}
		log.Fatalf("unable to find queue %v: %v", name, err.Error())
	}
	return aws.StringValue(resultURL.QueueUrl)
}

func newPoller(s sqsiface.SQSAPI, chn chan<- *sqs.Message, url string, interval int64) {
	for {
		log.Debugln("polling for messages from queue")
		output, err := s.ReceiveMessage(&sqs.ReceiveMessageInput{
			QueueUrl: aws.String(url),
			AttributeNames: aws.StringSlice([]string{
				"SenderId",
			}),
			MaxNumberOfMessages: aws.Int64(1),
			WaitTimeSeconds:     aws.Int64(interval),
		})
		if err != nil {
			log.Errorf("unable to receive message from queue %s, %v.", url, err)
			time.Sleep(time.Duration(interval) * time.Second)
		}
		if len(output.Messages) == 0 {
			log.Debugln("no messages received in interval")
		}
		for _, message := range output.Messages {
			chn <- message
		}
	}
}

func readMessage(message *sqs.Message) (*LifecycleEvent, error) {
	log.Debugf("reading message id=%v", aws.StringValue(message.MessageId))
	payload := []byte(aws.StringValue(message.Body))
	event := &LifecycleEvent{
		receiptHandle: aws.StringValue(message.ReceiptHandle),
	}
	err := json.Unmarshal(payload, event)
	if err != nil {
		return event, err
	}
	log.Debugf("unmarshalling event with message body %v", aws.StringValue(message.Body))
	return event, nil
}

func deleteMessage(sqsClient sqsiface.SQSAPI, url, receiptHandle string) error {
	log.Debugf("deleting message with receipt ID %v", receiptHandle)
	input := &sqs.DeleteMessageInput{
		QueueUrl:      aws.String(url),
		ReceiptHandle: aws.String(receiptHandle),
	}
	_, err := sqsClient.DeleteMessage(input)
	if err != nil {
		return err
	}
	return nil
}
