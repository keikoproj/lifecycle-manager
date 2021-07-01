# lifecycle-manager

[![Build Status](https://travis-ci.org/keikoproj/lifecycle-manager.svg?branch=master)](https://travis-ci.org/keikoproj/lifecycle-manager)
[![codecov](https://codecov.io/gh/keikoproj/lifecycle-manager/branch/master/graph/badge.svg)](https://codecov.io/gh/keikoproj/lifecycle-manager)
[![Go Report Card](https://goreportcard.com/badge/github.com/keikoproj/lifecycle-manager)](https://goreportcard.com/report/github.com/keikoproj/lifecycle-manager)
![version](https://img.shields.io/badge/version-0.5.0-green.svg?cacheSeconds=2592000)
> Graceful AWS scaling event on Kubernetes using lifecycle hooks

lifecycle-manager is a service that can be deployed to a Kubernetes cluster in order to make AWS autoscaling events more graceful using draining

Certain termination activities such as AZRebalance or TerminateInstanceInAutoScalingGroup API calls can cause autoscaling groups to terminate instances without having them properly drain first.

This can cause apps to experience errors when they are abruptly terminated.

lifecycle-manager uses lifecycle hooks from the autoscaling group (via SQS) to pre-drain the instances for you.

In addition to node draining, lifecycle-manager also tries to deregister the instance from any discovered ALB target group, this helps with pre-draining for the ALB instances prior to shutdown in order to avoid in-flight 5xx errors on your ALB - this feature is currently supported for `aws-alb-ingress-controller`.

## Usage

1. Configure your scaling groups to notify lifecycle-manager of terminations. you can use the provided enrollment CLI by running

```bash
$ make build
...

$ ./bin/lifecycle-manager enroll --region us-west-2 --queue-name lifecycle-manager-queue --notification-role-name my-notification-role --target-scaling-groups scaling-group-1,scaling-group-2 --overwrite

INFO[0000] starting enrollment for scaling groups [scaling-group-1 scaling-group-2]
INFO[0000] creating notification role 'my-notification-role'
INFO[0000] notification role 'my-notification-role' already exist, updating...
INFO[0000] attaching notification policy 'arn:aws:iam::aws:policy/service-role/AutoScalingNotificationAccessRole'
INFO[0001] created notification role 'arn:aws:iam::000000000000:role/my-notification-role'
INFO[0001] creating SQS queue 'lifecycle-manager-queue'
INFO[0001] created queue 'arn:aws:sqs:us-west-2:000000000000:lifecycle-manager-queue'
INFO[0001] creating lifecycle hook for 'scaling-group-1'
INFO[0002] creating lifecycle hook for 'scaling-group-2'
INFO[0002] successfully enrolled 2 scaling groups
INFO[0002] Queue Name: lifecycle-manager-queue
INFO[0002] Queue URL: https://sqs.us-west-2.amazonaws.com/000000000000/lifecycle-manager-queue
```

Alternatively, you can simply follow the [AWS docs](https://docs.aws.amazon.com/autoscaling/ec2/userguide/lifecycle-hooks.html#sqs-notifications) to create an SQS queue named `lifecycle-manager-queue`, a notification role, and a lifecycle-hook on your autoscaling group pointing to the created queue.

Configured scaling groups will now publish termination hooks to the SQS queue you created.

2. Deploy lifecycle-manager to your cluster:

```bash
kubectl create namespace lifecycle-manager

kubectl apply -f https://raw.githubusercontent.com/keikoproj/lifecycle-manager/master/examples/lifecycle-manager.yaml
```

Modifications may be needed if you used a different queue name than mentioned above

3. Kill an instance in your scaling group and watch it getting drained:

```bash
$ aws autoscaling terminate-instance-in-auto-scaling-group --instance-id i-0868736e381bf942a --region us-west-2 --no-should-decrement-desired-capacity
{
    "Activity": {
        "ActivityId": "5285b629-6a18-0a43-7c3c-f76bac8205f0",
        "AutoScalingGroupName": "scaling-group-1",
        "Description": "Terminating EC2 instance: i-0868736e381bf942a",
        "Cause": "At 2019-10-02T02:44:11Z instance i-0868736e381bf942a was taken out of service in response to a user request.",
        "StartTime": "2019-10-02T02:44:11.394Z",
        "StatusCode": "InProgress",
        "Progress": 0,
        "Details": "{\"Subnet ID\":\"subnet-0bf9bc85fEXAMPLE\",\"Availability Zone\":\"us-west-2c\"}"
    }
}

$ kubectl logs lifecycle-manager
time="2020-03-10T23:44:20Z" level=info msg="starting lifecycle-manager service v0.3.4"
time="2020-03-10T23:44:20Z" level=info msg="region = us-west-2"
time="2020-03-10T23:44:20Z" level=info msg="queue = lifecycle-manager-queue"
time="2020-03-10T23:44:20Z" level=info msg="polling interval seconds = 10"
time="2020-03-10T23:44:20Z" level=info msg="node drain timeout seconds = 300"
time="2020-03-10T23:44:20Z" level=info msg="node drain retry interval seconds = 30"
time="2020-03-10T23:44:20Z" level=info msg="with alb deregister = true"
time="2020-03-10T23:44:20Z" level=info msg="starting metrics server on /metrics:8080"
time="2020-03-11T07:24:37Z" level=info msg="i-0868736e381bf942a> received termination event"
time="2020-03-11T07:24:37Z" level=info msg="i-0868736e381bf942a> sending heartbeat (1/24)"
time="2020-03-11T07:24:37Z" level=info msg="i-0868736e381bf942a> draining node/ip-10-105-232-73.us-west-2.compute.internal"
time="2020-03-11T07:24:37Z" level=info msg="i-0868736e381bf942a> completed drain for node/ip-10-105-232-73.us-west-2.compute.internal"
time="2020-03-11T07:24:45Z" level=info msg="i-0868736e381bf942a> starting load balancer drain worker"
...
time="2020-03-11T07:24:49Z" level=info msg="event ce25c321-ec67-3f0b-c156-a7c1f75caf1a completed processing"
time="2020-03-11T07:24:49Z" level=info msg="i-0868736e381bf942a> setting lifecycle event as completed with result: CONTINUE"
time="2020-03-11T07:24:49Z" level=info msg="event ce25c321-ec67-3f0b-c156-a7c1f75caf1a for instance i-0868736e381bf942a completed after 12.054675203s"
```

### Required AWS Auth

```json
{
    "Effect": "Allow",
    "Action": [
        "autoscaling:DescribeLifecycleHooks",
        "autoscaling:CompleteLifecycleAction",
        "autoscaling:RecordLifecycleActionHeartbeat",
        "sqs:ReceiveMessage",
        "sqs:DeleteMessage",
        "sqs:GetQueueUrl",
        "ec2:DescribeSecurityGroups",
        "ec2:DescribeClassicLinkInstances",
        "ec2:DescribeInstances",
        "elasticloadbalancing:DeregisterInstancesFromLoadBalancer",
        "elasticloadbalancing:DescribeInstanceHealth",
        "elasticloadbalancing:DescribeLoadBalancers",
        "elasticloadbalancing:DeregisterTargets",
        "elasticloadbalancing:DescribeTargetHealth",
        "elasticloadbalancing:DescribeTargetGroups"
    ],
    "Resource": "*"
}
```

# Flags
| Name | Default | Type | Description |
|:------:|:---------:|:------:|:-------------:|
| local-mode | "" | String | absolute path to kubeconfig |
| region | "" | String | AWS region to operate in |
| queue-name | "" | String | the name of the SQS queue to consume lifecycle hooks from |
| kubectl-path | "/usr/local/bin/kubectl" | String | the path to kubectl binary |
| log-level | "info" | String | the logging level (info, warning, debug) |
| max-drain-concurrency | 32 | Int | maximum number of node drains to process in parallel |
| max-time-to-process | 3600 | Int | max time to spend processing an event |
| drain-timeout | 300 | Int | hard time limit for draining healthy nodes |
| drain-timeout-unknown | 30 | Int | hard time limit for draining nodes that are in unknown state |
| drain-interval | 30 | Int | interval in seconds for which to retry draining |
| polling-interval | 10 | Int | interval in seconds for which to poll SQS |
| with-deregister | true | Bool | try to deregister deleting instance from target groups |
| refresh-expired-credentials | false | Bool | refreshes expired credentials (requires shared credentials file) |


## Release History

Please see [CHANGELOG.md](.github/CHANGELOG.md).

## ❤ Contributing ❤

Please see [CONTRIBUTING.md](.github/CONTRIBUTING.md).

## Developer Guide

Please see [DEVELOPER.md](.github/DEVELOPER.md).
