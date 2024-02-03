# Changelog

## v0.6.3

* Bump github.com/spf13/cobra from 1.7.0 to 1.8.0 by @dependabot in #172
* Bump github.com/aws/aws-sdk-go from 1.47.11 to 1.48.11 by @dependabot in #174
* Bump github.com/keikoproj/inverse-exp-backoff from 0.0.3 to 0.0.4 by @dependabot in #175
* Bump k8s.io/api from 0.25.15 to 0.25.16 by @dependabot in #173
* Increase qps and burst to 100 by @ZihanJiang96 in #184

## v0.6.2

**NOTE:** Beginning with this release, the docker image tag for `lifecycle-manager` will be prefixed with `v`. For example, `v0.6.2` instead of `0.6.2`. This is to align with the [Semantic Versioning](https://semver.org/) specification.

### Fixed
* Improve dependabot configuration by @tekenstam in https://github.com/keikoproj/lifecycle-manager/pull/154
* use the copied node obj to drain the node by @ZihanJiang96 in https://github.com/keikoproj/lifecycle-manager/pull/165

### What's Changed
* Bump github.com/avast/retry-go from 2.4.1+incompatible to 2.7.0+incompatible by @dependabot in https://github.com/keikoproj/lifecycle-manager/pull/110
* Bump golang.org/x/sync from 0.0.0-20190911185100-cd5d95a43a6e to 0.3.0 by @dependabot in https://github.com/keikoproj/lifecycle-manager/pull/120
* Bump github.com/emicklei/go-restful from 2.9.5+incompatible to 2.16.0+incompatible by @dependabot in https://github.com/keikoproj/lifecycle-manager/pull/123
* Bump github.com/spf13/cobra from 1.1.1 to 1.7.0 by @dependabot in https://github.com/keikoproj/lifecycle-manager/pull/125
* Bump github.com/aws/aws-sdk-go from 1.25.0 to 1.45.9 by @dependabot in https://github.com/keikoproj/lifecycle-manager/pull/134
* Bump actions/checkout from 3 to 4 by @dependabot in https://github.com/keikoproj/lifecycle-manager/pull/138
* Bump codecov/codecov-action from 3 to 4 by @dependabot in https://github.com/keikoproj/lifecycle-manager/pull/140
* Update to codecov@v3 unit-test.yaml by @tekenstam in https://github.com/keikoproj/lifecycle-manager/pull/142
* Bump docker/login-action from 2 to 3 by @dependabot in https://github.com/keikoproj/lifecycle-manager/pull/141
* Bump docker/metadata-action from 4 to 5 by @dependabot in https://github.com/keikoproj/lifecycle-manager/pull/137
* Bump docker/build-push-action from 4 to 5 by @dependabot in https://github.com/keikoproj/lifecycle-manager/pull/136
* Update to Golang 1.19 by @tekenstam in https://github.com/keikoproj/lifecycle-manager/pull/148
* Bump github.com/sirupsen/logrus from 1.6.0 to 1.9.3 by @dependabot in https://github.com/keikoproj/lifecycle-manager/pull/143
* Bump k8s.io/kubectl from 0.20.4 to 0.20.15 by @dependabot in https://github.com/keikoproj/lifecycle-manager/pull/146
* Bump docker/setup-qemu-action from 2 to 3 by @dependabot in https://github.com/keikoproj/lifecycle-manager/pull/135
* Bump docker/setup-buildx-action from 2 to 3 by @dependabot in https://github.com/keikoproj/lifecycle-manager/pull/139
* Bump github.com/aws/aws-sdk-go from 1.45.9 to 1.45.16 by @dependabot in https://github.com/keikoproj/lifecycle-manager/pull/152
* Bump github.com/prometheus/client_golang from 1.7.1 to 1.16.0 by @dependabot in https://github.com/keikoproj/lifecycle-manager/pull/145
* Update aws-sdk-go-cache and inverse-exp-backoff by @tekenstam in https://github.com/keikoproj/lifecycle-manager/pull/153
* Update client-go and kubectl to v0.25.14 by @tekenstam in https://github.com/keikoproj/lifecycle-manager/pull/157
* Bump github.com/aws/aws-sdk-go from 1.45.16 to 1.45.18 by @dependabot in https://github.com/keikoproj/lifecycle-manager/pull/156
* Bump github.com/prometheus/client_golang from 1.16.0 to 1.17.0 by @dependabot in https://github.com/keikoproj/lifecycle-manager/pull/155
* Bump golang.org/x/net from 0.12.0 to 0.17.0 by @dependabot in https://github.com/keikoproj/lifecycle-manager/pull/159
* Bump k8s.io/client-go from 0.25.14 to 0.25.15 by @dependabot in https://github.com/keikoproj/lifecycle-manager/pull/160
* Bump golang.org/x/sync from 0.3.0 to 0.5.0 by @dependabot in https://github.com/keikoproj/lifecycle-manager/pull/166
* Bump github.com/aws/aws-sdk-go from 1.45.18 to 1.47.11 by @dependabot in https://github.com/keikoproj/lifecycle-manager/pull/167
* Add keiko-admins and keiko-maintainers as codeowners by @tekenstam in https://github.com/keikoproj/lifecycle-manager/pull/168
* Update kubectl to v0.25.15 by @tekenstam in https://github.com/keikoproj/lifecycle-manager/pull/169

### New Contributors
* @tekenstam made their first contribution in https://github.com/keikoproj/lifecycle-manager/pull/142
* @ZihanJiang96 made their first contribution in https://github.com/keikoproj/lifecycle-manager/pull/165

**Full Changelog**: https://github.com/keikoproj/lifecycle-manager/compare/0.6.1...v0.6.2


## 0.6.1
+ Fix broken build badge (#108)
+ Include draintimeout and change node drain method. (#121) 
+ Node deletion (#127)

## 0.6.0

+ Bug fix: Allow drain retries even with timeout failures (#104)
+ Package version update: Update docker actions to v4 (#106)
+ Package version update: Build updates, dependabot + GH Actions (#103)
+ Deperecate: Cleanup vendors (#94)
+ Package version update: Bump docker/login-action from 1 to 2 (#97)
+ Package version update: Update builders (#96)

## 0.5.1

+ Support cross-compliation for arm (#76)

## 0.5.0

+ Documentation fixes (#67)
+ Update kubectl binary to 1.18.14 (#68)
+ Allow lifecycle events longer than 1hr (#69)
+ Annotate node with queue URL (#71)
+ Move to Github Actions (#73)
+ Support Kubernetes 1.19 exclusion labels (#72)

## 0.4.3

+ Fix deadlock when resuming in-progress terminations (#63)

## 0.4.2

+ Update kubectl to 1.16.5 (#60)

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
