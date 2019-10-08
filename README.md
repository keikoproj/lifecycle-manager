# lifecycle-manager

[![Build Status](https://travis-ci.org/keikoproj/lifecycle-manager.svg?branch=master)](https://travis-ci.org/keikoproj/lifecycle-manager)
[![codecov](https://codecov.io/gh/keikoproj/lifecycle-manager/branch/master/graph/badge.svg)](https://codecov.io/gh/keikoproj/lifecycle-manager)
[![Go Report Card](https://goreportcard.com/badge/github.com/keikoproj/lifecycle-manager)](https://goreportcard.com/report/github.com/keikoproj/lifecycle-manager)
> Graceful AWS scaling event on Kubernetes using lifecycle hooks

lifecycle-manager is a service that can be deployed to a Kubernetes cluster in order to make AWS autoscaling events more graceful using draining

Certain termination activities such as AZRebalance or TerminateInstanceInAutoScalingGroup API calls can cause autoscaling groups to terminate instances without having them properly drain first.

This can cause apps to experience errors when they are abruptly terminated.

lifecycle-manager uses lifecycle hooks from the autoscaling group (via SQS) to pre-drain the instances for you.

## Usage

1. Follow the [AWS docs](https://docs.aws.amazon.com/autoscaling/ec2/userguide/lifecycle-hooks.html#sqs-notifications) to create an SQS queue named `lifecycle-manager-queue`, a notification role, and a lifecycle-hook on your autoscaling group pointing to the created queue.

2. Deploy lifecycle-manager to your cluster:

```bash
kubectl create namespace lifecycle-manager

kubectl apply -f https://raw.githubusercontent.com/keikoproj/lifecycle-manager/master/examples/lifecycle-manager.yaml
```

3. Kill an instance in your scaling group and watch it getting drained:

```bash
$ aws autoscaling terminate-instance-in-auto-scaling-group --instance-id i-0d3ba307bc6cebeda --region us-west-2 --no-should-decrement-desired-capacity
{
    "Activity": {
        "ActivityId": "5285b629-6a18-0a43-7c3c-f76bac8205f0",
        "AutoScalingGroupName": "my-scaling-group",
        "Description": "Terminating EC2 instance: i-0d3ba307bc6cebeda",
        "Cause": "At 2019-10-02T02:44:11Z instance i-0d3ba307bc6cebeda was taken out of service in response to a user request.",
        "StartTime": "2019-10-02T02:44:11.394Z",
        "StatusCode": "InProgress",
        "Progress": 0,
        "Details": "{\"Subnet ID\":\"subnet-0bf9bc85fEXAMPLE\",\"Availability Zone\":\"us-west-2c\"}"
    }
}

$ kubectl logs lifecycle-manager
time="2019-10-02T02:44:05Z" level=info msg="starting lifecycle-manager service v0.2.0"
time="2019-10-02T02:44:05Z" level=info msg="region = us-west-2"
time="2019-10-02T02:44:05Z" level=info msg="queue = https://sqs.us-west-2.amazonaws.com/00000EXAMPLE/lifecycle-manager-queue"
time="2019-10-02T02:44:05Z" level=info msg="polling interval seconds = 10"
time="2019-10-02T02:44:05Z" level=info msg="drain timeout seconds = 300"
time="2019-10-02T02:44:05Z" level=info msg="drain retry interval seconds = 30"
time="2019-10-02T02:44:05Z" level=info msg="spawning sqs poller"
time="2019-10-02T02:44:12Z" level=info msg="spawning event handler"
time="2019-10-02T02:44:12Z" level=info msg="hook heartbeat timeout interval is 60, will send heartbeat every 30 seconds"
time="2019-10-02T02:44:12Z" level=info msg="draining node ip-10-10-10-10.us-west-2.compute.internal"
time="2019-10-02T02:44:42Z" level=info msg="sending heartbeat for event with instance 'i-0d3ba307bc6cebeda' and sleeping for 30 seconds"
time="2019-10-02T02:44:45Z" level=info msg="completed drain for node 'ip-10-10-10-10.us-west-2.compute.internal'"
time="2019-10-02T02:44:45Z" level=info msg="setting lifecycle event as completed with result: 'CONTINUE'"
```

## Release History

Please see [CHANGELOG.md](.github/CHANGELOG.md).

## ❤ Contributing ❤

Please see [CONTRIBUTING.md](.github/CONTRIBUTING.md).

## Developer Guide

Please see [DEVELOPER.md](.github/DEVELOPER.md).
