package service

import (
	"encoding/json"

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

func readMessage(message *sqs.Message) (*LifecycleEvent, error) {
	var (
		event   = &LifecycleEvent{}
		receipt = aws.StringValue(message.ReceiptHandle)
		body    = aws.StringValue(message.Body)
	)
	log.Debugf("reading message id=%v", aws.StringValue(message.MessageId))
	event.SetReceiptHandle(receipt)
	err := json.Unmarshal([]byte(body), event)
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
