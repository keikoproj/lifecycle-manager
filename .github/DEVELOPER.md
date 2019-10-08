# Development reference

This document will walk you through setting up a basic testing environment, running unit tests and testing changes locally.
This document will also assume a running Kubernetes cluster on AWS.

## Running locally

You can run the `main.go` file with the appropriate command line arguments to invoke lifecycle-manager locally.
The `--local-mode` flag tells lifecycle-manager to use a local kubeconfig from the provided path instead of `InClusterAuth`.

Make sure you already have an autoscaling group configured to post lifecycle hooks to an SQS queue.

### Example

```bash
$ make build
$ ./bin/lifecycle-manager serve --kubectl-path /usr/local/bin/kubectl --local-mode /path/to/.kube/config --queue-name my-queue --region us-west-2

time="2019-09-28T05:15:58Z" level=info msg="starting lifecycle-manager service v0.2.0"
time="2019-09-28T05:15:58Z" level=info msg="region = us-west-2"
time="2019-09-28T05:15:58Z" level=info msg="queue = https://sqs.us-west-2.amazonaws.com/123456789012/my-queue"
time="2019-09-28T05:15:58Z" level=info msg="polling interval seconds = 10"
time="2019-09-28T05:15:58Z" level=info msg="drain timeout seconds = 300"
time="2019-09-28T05:15:58Z" level=info msg="spawning sqs poller"
time="2019-09-28T05:15:58Z" level=debug msg="polling for messages from queue"
```

Any terminating lifecycle hook sent to `my-queue` SQS queue, will now be processed by lifecycle-manager and nodes will be pre-drained.

## Running unit tests

Using the `Makefile` you can run basic unit tests.

### Example

```bash
$ make test
go test ./... -coverprofile ./coverage.txt
?       github.com/keikoproj/lifecycle-manager  [no test files]
?       github.com/keikoproj/lifecycle-manager/cmd  [no test files]
?       github.com/keikoproj/lifecycle-manager/pkg/log  [no test files]
ok      github.com/keikoproj/lifecycle-manager/pkg/service  6.347s  coverage: 63.8% of statements
?       github.com/keikoproj/lifecycle-manager/pkg/version  [no test files]
go tool cover -html=./coverage.txt -o cover.html
```
