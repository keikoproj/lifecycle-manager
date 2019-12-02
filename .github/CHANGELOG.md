# Changelog

## 0.3.2

+ Better naming for event reasons (#32)
+ Expose prometheus metrics (#29)

## 0.3.1

+ Documentation fixes (#19, #20)
+ Logging improvements and fixes (#21, #16)
+ Event publishing improvements (#26)
+ Better mechanism for AWS API calls to avoid being throttled (#25)
+ Bug fix: complete events when they fail (#27)

## 0.3.0

+ Improved error handling
+ Support classic-elb deregistration
+ Kubernetes event publishing

## 0.2.0

+ Support `--with-deregister` flag to deregister ALB instances
+ Support `--log-level` flag to set the logging verbosity
+ Add pagination and retries in AWS calls

## 0.1.0

+ Initial alpha release
