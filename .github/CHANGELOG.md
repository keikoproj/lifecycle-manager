# Changelog

## 0.4.1

+ Add option for refreshing expired AWS token (#57)
+ add Kind, firsttimestamp and count to v1.Event (#56)
+ Refactor message validation (#55)
+ Enrollment CLI (#53)

## 0.4.0

+ Separate drain timeout for nodes in Unknown state (#51)
+ Bucket drain events using semaphore (#49)

## 0.3.4

+ More efficient deregistration (#42)
+ Logging improvements (#42)
+ Waiters - use inverse exponential backoff (#42)
+ Error handling improvements (#42)
+ No cache flushing on DeregisterInstances (#42)

## 0.3.3

+ Bugfix: Proceed with drain failure (#37)
+ Bugfix: Drop goroutines when instance abandoned (#34)
+ Idempotency - resume operations after pod restart (#35)
+ API Caching - cache AWS calls to improve performance (#35)

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
