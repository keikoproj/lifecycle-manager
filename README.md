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

```sh
kubectl create namespace lifecycle-manager

kubectl apply -f https://raw.githubusercontent.com/keikoproj/lifecycle-manager/master/examples/lifecycle-manager.yaml
```

## Release History

* 0.1.0
  * Release alpha version of lifecycle-manager

## ❤ Contributing ❤

Please see [CONTRIBUTING.md](.github/CONTRIBUTING.md).

## Developer Guide

Please see [DEVELOPER.md](.github/DEVELOPER.md).
