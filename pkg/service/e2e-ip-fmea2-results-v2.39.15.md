---

## Test 3: Second FMEA overnight test (April 23, 2026)

Independent repeat run on the same `ip-fmea1-e2e-usw2-k8s` cluster, different instance and node, confirms the graceful-termination path is repeatable under identical production-like settings.

### Setup
```
lifecycle manager image used: docker.intuit.com/docker-rmt/keikoproj/lifecycle-manager:v0.6.6
cluster: ip-fmea1-e2e-usw2-k8s
namespace: sandbox-sandbox-draychevcustgitprima-usw2-e2e
pod: drain-blocker-54c6f7cc8c-mlvmw
    running on node: ip-10-235-117-80.us-west-2.compute.internal
Instance id: i-0d3dbac803fff8c33
ASG: ip-fmea1-e2e-usw2-k8s-instance-manager-nodes
Availability Zone: us-west-2c (usw2-az3)
Termination initiated: 2026-04-23T06:01:27Z (user-requested scale-in)

container settings:
     containers:
      - command:
        - /bin/lifecycle-manager
        - serve
        - --queue-name=ip-fmea1-e2e-usw2-k8s-lifecycle-manager
        - --region=us-west-2
        - --polling-interval=10
        - --drain-timeout=3600
        - --drain-timeout-unknown=60
        - --max-time-to-process=10800
        - --max-drain-concurrency=32
        - --max-termination-grace-period=900
        - --with-deregister=true
        - --deregister-target-types=classic-elb,target-group
        - --log-level=debug
        env:
        - name: AWS_USE_FIPS_ENDPOINT
          value: "true"
        image: docker.intuit.com/docker-rmt/keikoproj/lifecycle-manager:v0.6.6
        imagePullPolicy: Always
        name: lifecycle-manager
```

### Test Result

**Event — full heartbeat-timeout graceful termination (~3h)**
```
06:01:28  Event received, drain starts (blocked by PDB)
          Heartbeats every 2.5 min (1/72 through 71/72)
          Drain retrying for ~3 hours...
          (drain-timeout retries visible at ~07:01:33 and ~08:01:38)

08:59:07  Heartbeat 72/72 → TIMEOUT after 2h57m39s
          → deleting pod (grace period: 600s — pod's 600s within 900s global cap)
          → requested deletion of 1 non-daemonset pods
          → publishing event: HeartbeatTimeoutGracefulTermination

08:59:39  Drain succeeded (pod deletion unblocked the PDB)
08:59:59  Node deleted, event completed with CONTINUE (total: 10710.93s ≈ 2h58m31s)
09:00:37  "event already completed by main goroutine, skipping ABANDON"
```

### Validation matrix
| Check | Evidence | Result |
|---|---|---|
| Event received | `i-0d3dbac803fff8c33> received termination event` at 06:01:28Z | Pass |
| 71 heartbeats over ~3 h | `sending heartbeat (1/72)` → `(71/72)` at 2.5 min intervals | Pass |
| PDB blocked drain | `Cannot evict pod ... disruption budget` retries for ~3 hours | Pass |
| Timeout fired at correct time | `heartbeat extended over threshold after 2h57m39s` at 08:59:07Z | Pass |
| Grace period within cap | `(grace period: 600s)` — pod had 600s, global cap 900s | Pass |
| K8s event published | `publishing event: HeartbeatTimeoutGracefulTermination` | Pass |
| Drain unblocked | `drain succeeded` at 08:59:39Z | Pass |
| LB deregister | Scanned target groups + classic ELBs, 0 active targets | Pass |
| Node deleted | `completed node deletion` at 08:59:59Z | Pass |
| Event completed with CONTINUE | `setting lifecycle event as completed with result: CONTINUE` | Pass |
| Total time | `10710.93s` (≈ 2h58m31s) | Pass |
| ABANDON race guarded | `event already completed by main goroutine, skipping ABANDON` | Pass |

### AWS Scale-in Activity
```json
{
    "Activity": {
        "ActivityId": "0de676e3-b756-0ebd-beda-a29764aec760",
        "AutoScalingGroupName": "ip-fmea1-e2e-usw2-k8s-instance-manager-nodes",
        "Description": "Terminating EC2 instance: i-0d3dbac803fff8c33",
        "Cause": "At 2026-04-23T06:01:27Z instance i-0d3dbac803fff8c33 was taken out of service in response to a user request.",
        "StartTime": "2026-04-23T06:01:27.683000+00:00",
        "StatusCode": "InProgress",
        "Progress": 0,
        "Details": "{\"Availability Zone ID\":\"usw2-az3\",\"Subnet ID\":\"subnet-0a9f4a492da03e0fa\",\"Availability Zone\":\"us-west-2c\"}"
    }
}
```

### Lifecycle manager logs
```
kubectl logs lifecycle-manager-744c89cf46-wqpsv -n addon-lifecycle-manager-ns | grep -E "received termination|heartbeat|threshold|deleting pod|requested deletion|ABANDON|CONTINUE|completed|skipping|grace period|publishing event"
time="2026-04-23T06:01:28Z" level=info msg="i-0d3dbac803fff8c33> received termination event"
time="2026-04-23T06:01:28Z" level=info msg="i-0d3dbac803fff8c33> sending heartbeat (1/72)"
time="2026-04-23T06:03:58Z" level=info msg="i-0d3dbac803fff8c33> sending heartbeat (2/72)"
time="2026-04-23T06:06:28Z" level=info msg="i-0d3dbac803fff8c33> sending heartbeat (3/72)"
time="2026-04-23T06:08:59Z" level=info msg="i-0d3dbac803fff8c33> sending heartbeat (4/72)"
time="2026-04-23T06:11:29Z" level=info msg="i-0d3dbac803fff8c33> sending heartbeat (5/72)"
time="2026-04-23T06:13:59Z" level=info msg="i-0d3dbac803fff8c33> sending heartbeat (6/72)"
time="2026-04-23T06:16:29Z" level=info msg="i-0d3dbac803fff8c33> sending heartbeat (7/72)"
time="2026-04-23T06:18:59Z" level=info msg="i-0d3dbac803fff8c33> sending heartbeat (8/72)"
time="2026-04-23T06:21:29Z" level=info msg="i-0d3dbac803fff8c33> sending heartbeat (9/72)"
time="2026-04-23T06:23:59Z" level=info msg="i-0d3dbac803fff8c33> sending heartbeat (10/72)"
time="2026-04-23T06:26:30Z" level=info msg="i-0d3dbac803fff8c33> sending heartbeat (11/72)"
time="2026-04-23T06:29:00Z" level=info msg="i-0d3dbac803fff8c33> sending heartbeat (12/72)"
time="2026-04-23T06:31:30Z" level=info msg="i-0d3dbac803fff8c33> sending heartbeat (13/72)"
time="2026-04-23T06:34:00Z" level=info msg="i-0d3dbac803fff8c33> sending heartbeat (14/72)"
time="2026-04-23T06:36:30Z" level=info msg="i-0d3dbac803fff8c33> sending heartbeat (15/72)"
time="2026-04-23T06:39:00Z" level=info msg="i-0d3dbac803fff8c33> sending heartbeat (16/72)"
time="2026-04-23T06:41:30Z" level=info msg="i-0d3dbac803fff8c33> sending heartbeat (17/72)"
time="2026-04-23T06:44:00Z" level=info msg="i-0d3dbac803fff8c33> sending heartbeat (18/72)"
time="2026-04-23T06:46:31Z" level=info msg="i-0d3dbac803fff8c33> sending heartbeat (19/72)"
time="2026-04-23T06:49:01Z" level=info msg="i-0d3dbac803fff8c33> sending heartbeat (20/72)"
time="2026-04-23T06:51:31Z" level=info msg="i-0d3dbac803fff8c33> sending heartbeat (21/72)"
time="2026-04-23T06:54:01Z" level=info msg="i-0d3dbac803fff8c33> sending heartbeat (22/72)"
time="2026-04-23T06:56:31Z" level=info msg="i-0d3dbac803fff8c33> sending heartbeat (23/72)"
time="2026-04-23T06:59:01Z" level=info msg="i-0d3dbac803fff8c33> sending heartbeat (24/72)"
time="2026-04-23T07:01:31Z" level=info msg="i-0d3dbac803fff8c33> sending heartbeat (25/72)"
time="2026-04-23T07:04:01Z" level=info msg="i-0d3dbac803fff8c33> sending heartbeat (26/72)"
time="2026-04-23T07:06:32Z" level=info msg="i-0d3dbac803fff8c33> sending heartbeat (27/72)"
time="2026-04-23T07:09:02Z" level=info msg="i-0d3dbac803fff8c33> sending heartbeat (28/72)"
time="2026-04-23T07:11:32Z" level=info msg="i-0d3dbac803fff8c33> sending heartbeat (29/72)"
time="2026-04-23T07:14:02Z" level=info msg="i-0d3dbac803fff8c33> sending heartbeat (30/72)"
time="2026-04-23T07:16:32Z" level=info msg="i-0d3dbac803fff8c33> sending heartbeat (31/72)"
time="2026-04-23T07:19:02Z" level=info msg="i-0d3dbac803fff8c33> sending heartbeat (32/72)"
time="2026-04-23T07:21:32Z" level=info msg="i-0d3dbac803fff8c33> sending heartbeat (33/72)"
time="2026-04-23T07:24:02Z" level=info msg="i-0d3dbac803fff8c33> sending heartbeat (34/72)"
time="2026-04-23T07:26:33Z" level=info msg="i-0d3dbac803fff8c33> sending heartbeat (35/72)"
time="2026-04-23T07:29:03Z" level=info msg="i-0d3dbac803fff8c33> sending heartbeat (36/72)"
time="2026-04-23T07:31:33Z" level=info msg="i-0d3dbac803fff8c33> sending heartbeat (37/72)"
time="2026-04-23T07:34:03Z" level=info msg="i-0d3dbac803fff8c33> sending heartbeat (38/72)"
time="2026-04-23T07:36:33Z" level=info msg="i-0d3dbac803fff8c33> sending heartbeat (39/72)"
time="2026-04-23T07:39:03Z" level=info msg="i-0d3dbac803fff8c33> sending heartbeat (40/72)"
time="2026-04-23T07:41:33Z" level=info msg="i-0d3dbac803fff8c33> sending heartbeat (41/72)"
time="2026-04-23T07:44:04Z" level=info msg="i-0d3dbac803fff8c33> sending heartbeat (42/72)"
time="2026-04-23T07:46:34Z" level=info msg="i-0d3dbac803fff8c33> sending heartbeat (43/72)"
time="2026-04-23T07:49:04Z" level=info msg="i-0d3dbac803fff8c33> sending heartbeat (44/72)"
time="2026-04-23T07:51:34Z" level=info msg="i-0d3dbac803fff8c33> sending heartbeat (45/72)"
time="2026-04-23T07:54:04Z" level=info msg="i-0d3dbac803fff8c33> sending heartbeat (46/72)"
time="2026-04-23T07:56:34Z" level=info msg="i-0d3dbac803fff8c33> sending heartbeat (47/72)"
time="2026-04-23T07:59:04Z" level=info msg="i-0d3dbac803fff8c33> sending heartbeat (48/72)"
time="2026-04-23T08:01:34Z" level=info msg="i-0d3dbac803fff8c33> sending heartbeat (49/72)"
time="2026-04-23T08:04:05Z" level=info msg="i-0d3dbac803fff8c33> sending heartbeat (50/72)"
time="2026-04-23T08:06:35Z" level=info msg="i-0d3dbac803fff8c33> sending heartbeat (51/72)"
time="2026-04-23T08:09:05Z" level=info msg="i-0d3dbac803fff8c33> sending heartbeat (52/72)"
time="2026-04-23T08:11:35Z" level=info msg="i-0d3dbac803fff8c33> sending heartbeat (53/72)"
time="2026-04-23T08:14:05Z" level=info msg="i-0d3dbac803fff8c33> sending heartbeat (54/72)"
time="2026-04-23T08:16:35Z" level=info msg="i-0d3dbac803fff8c33> sending heartbeat (55/72)"
time="2026-04-23T08:19:05Z" level=info msg="i-0d3dbac803fff8c33> sending heartbeat (56/72)"
time="2026-04-23T08:21:35Z" level=info msg="i-0d3dbac803fff8c33> sending heartbeat (57/72)"
time="2026-04-23T08:24:05Z" level=info msg="i-0d3dbac803fff8c33> sending heartbeat (58/72)"
time="2026-04-23T08:26:36Z" level=info msg="i-0d3dbac803fff8c33> sending heartbeat (59/72)"
time="2026-04-23T08:29:06Z" level=info msg="i-0d3dbac803fff8c33> sending heartbeat (60/72)"
time="2026-04-23T08:31:36Z" level=info msg="i-0d3dbac803fff8c33> sending heartbeat (61/72)"
time="2026-04-23T08:34:06Z" level=info msg="i-0d3dbac803fff8c33> sending heartbeat (62/72)"
time="2026-04-23T08:36:36Z" level=info msg="i-0d3dbac803fff8c33> sending heartbeat (63/72)"
time="2026-04-23T08:39:06Z" level=info msg="i-0d3dbac803fff8c33> sending heartbeat (64/72)"
time="2026-04-23T08:41:36Z" level=info msg="i-0d3dbac803fff8c33> sending heartbeat (65/72)"
time="2026-04-23T08:44:07Z" level=info msg="i-0d3dbac803fff8c33> sending heartbeat (66/72)"
time="2026-04-23T08:46:37Z" level=info msg="i-0d3dbac803fff8c33> sending heartbeat (67/72)"
time="2026-04-23T08:49:07Z" level=info msg="i-0d3dbac803fff8c33> sending heartbeat (68/72)"
time="2026-04-23T08:51:37Z" level=info msg="i-0d3dbac803fff8c33> sending heartbeat (69/72)"
time="2026-04-23T08:54:07Z" level=info msg="i-0d3dbac803fff8c33> sending heartbeat (70/72)"
time="2026-04-23T08:56:37Z" level=info msg="i-0d3dbac803fff8c33> sending heartbeat (71/72)"
time="2026-04-23T08:59:07Z" level=warning msg="i-0d3dbac803fff8c33> heartbeat extended over threshold after 2h57m39s, beginning graceful pod termination"
time="2026-04-23T08:59:07Z" level=info msg="deleting pod sandbox-sandbox-draychevcustgitprima-usw2-e2e/drain-blocker-54c6f7cc8c-mlvmw on node ip-10-235-117-80.us-west-2.compute.internal (grace period: 600s)"
time="2026-04-23T08:59:07Z" level=info msg="i-0d3dbac803fff8c33> requested deletion of 1 non-daemonset pods on node ip-10-235-117-80.us-west-2.compute.internal"
time="2026-04-23T08:59:07Z" level=debug msg="publishing event: HeartbeatTimeoutGracefulTermination"
time="2026-04-23T08:59:07Z" level=info msg="i-0d3dbac803fff8c33> waiting for 1 non-daemonset pods to terminate on node ip-10-235-117-80.us-west-2.compute.internal"
time="2026-04-23T08:59:39Z" level=info msg="drain succeeded, node ip-10-235-117-80.us-west-2.compute.internal"
time="2026-04-23T08:59:39Z" level=info msg="i-0d3dbac803fff8c33> completed drain for node/ip-10-235-117-80.us-west-2.compute.internal"
time="2026-04-23T08:59:59Z" level=info msg="i-0d3dbac803fff8c33> completed node deletion/ip-10-235-117-80.us-west-2.compute.internal"
time="2026-04-23T08:59:59Z" level=info msg="i-0d3dbac803fff8c33> setting lifecycle event as completed with result: CONTINUE"
time="2026-04-23T08:59:59Z" level=info msg="event fc367673-268e-b16d-e939-f4635880b866 for instance i-0d3dbac803fff8c33 completed after 10710.93171229s"
time="2026-04-23T09:00:37Z" level=error msg="i-0d3dbac803fff8c33> failed to send heartbeat during termination grace period: ValidationError: No active Lifecycle Action found with token\n\tstatus code: 400"
time="2026-04-23T09:00:37Z" level=info msg="i-0d3dbac803fff8c33> event already completed by main goroutine, skipping ABANDON"
```

---

## Test 6: Stubborn pod that ignores SIGTERM beyond the cap (April 23, 2026)

Adversarial run where the pod declares `terminationGracePeriodSeconds: 1200` (higher than the LM cap of `900s`) **and** installs a `SIGTERM` trap that sleeps `1500s` — longer than both the pod's declared grace and the LM's global cap. Intent: observe who force-kills first when the pod refuses to exit.

### Setup
```
lifecycle manager image used: docker.intuit.com/docker-rmt/keikoproj/lifecycle-manager:v0.6.6
cluster: ip-fmea1-e2e-usw2-k8s
namespace: sandbox-sandbox-draychevcustgitprima-usw2-e2e
pod: drain-blocker-98cc7fbb9-hrpjd
    running on node: ip-10-235-115-212.us-west-2.compute.internal
Instance id: i-0d445af41a1a6df3a
ASG: ip-fmea1-e2e-usw2-k8s-instance-manager-nodes
Availability Zone: us-west-2b
Termination initiated: 2026-04-23T23:40:14Z (user-requested scale-in)

pod spec:
    spec:
      terminationGracePeriodSeconds: 1200   # 20 min (larger than LM cap of 900s)
      containers:
      - name: sleeper
        command:
        - sh
        - -c
        - |
          trap 'echo "SIGTERM received at $(date) — INTENTIONALLY sleeping 1500s to exceed 900s cap"; sleep 1500; exit 0' TERM
          while true; do sleep 1; done
    # PDB: maxUnavailable: 0, selector matches drain-blocker

container settings (lifecycle-manager — same as Tests 3/5, fully production values):
     containers:
     - command:
       - /bin/lifecycle-manager
       - serve
       - --queue-name=ip-fmea1-e2e-usw2-k8s-lifecycle-manager
       - --region=us-west-2
       - --polling-interval=10
       - --drain-timeout=3600                # 1 h per drain attempt
       - --drain-timeout-unknown=60
       - --max-time-to-process=10800         # 3 h overall heartbeat window
       - --max-drain-concurrency=32
       - --max-termination-grace-period=900  # 15 min cap on per-pod grace
       - --with-deregister=true
       - --deregister-target-types=classic-elb,target-group
       - --log-level=debug
       image: docker.intuit.com/docker-rmt/keikoproj/lifecycle-manager:v0.6.6
       name: lifecycle-manager
```

### Test Result

**Event — ABANDON due to `drain-timeout` preempting the 900s Kubernetes force-kill deadline**
```
23:40:14Z  Event received, drain starts (blocked by PDB maxUnavailable: 0)
           Heartbeats every 2.5 min (1/72 → 71/72)
           drain-timeout=3600s expires; LM retries drain:
             00:40:15Z  "global timeout reached: 1h0m0s" → retry #1
             01:40:20Z  "global timeout reached: 1h0m0s" → retry #2

02:37:53Z  Heartbeat 72/72 → max-time-to-process TIMEOUT after 2h57m39s
           → deleting pod (grace period: 900s — capped from pod's 1200s)
           → publishing event: HeartbeatTimeoutGracefulTermination
           → Kubernetes DELETE issued; K8s force-kill deadline = 02:52:53Z

02:40:20Z  (2m 27s after deletePod) current drain attempt hits
           "global timeout reached: 1h0m0s" again
           → "error when waiting for pod ... to terminate: context deadline exceeded"
           → LM has exhausted its drain retries within the 3h window

02:40:38Z  LM sets lifecycle event as ABANDON after 10823.99s (~3h0m24s)
           → ASG now owns the termination; instance goes to Terminating

02:40:42Z+ Background goroutine keeps trying to heartbeat; API rejects:
           "No active Lifecycle Action found with token ..." (hook already abandoned)

02:45:06Z  Pod finally gone (~7m13s after deletePod, well inside the 900s cap)
           → "all non-daemonset pods terminated on node"
           → "event already completed by main goroutine, skipping ABANDON"
```

### What actually force-killed the stubborn pod?

Not Kubernetes — **the ASG did**. Because LM returned `ABANDON`, the ASG proceeded to terminate `i-0d445af41a1a6df3a` immediately. The node (kubelet) stopped running before Kubernetes' 900s force-kill deadline at 02:52:53Z, so the pod object disappeared as a side effect of the node going away at ~02:45:06Z — not from K8s SIGKILL at the capped deadline.

### Key finding: `drain-timeout` can preempt `max-termination-grace-period`

There is a timing interaction between two independent timers once heartbeat timeout triggers graceful deletion:

| Timer | Value | Scope |
|---|---|---|
| `max-time-to-process` | 10800s (3h) | Whole lifecycle event; triggers heartbeat-timeout graceful deletion |
| `max-termination-grace-period` | 900s (15m) | Cap applied to pod's `terminationGracePeriodSeconds` on DELETE |
| `drain-timeout` | 3600s (1h) | Each `kubectl drain` attempt; retried on failure |

When the heartbeat timeout fires late in the 3h window (here at 2h57m39s), the LM issues `DELETE pod --grace-period=900`, then re-enters `waitForPodsWithHeartbeat` — but that wait is bounded by the **current drain attempt's** 1h `drain-timeout`, which in this run had only 2m 27s remaining. The wait ctx expires at 02:40:20Z, long before the 900s K8s deadline at 02:52:53Z, and LM marks the event `ABANDON`.

### Validation matrix
| Check | Evidence | Result |
|---|---|---|
| Event received | `i-0d445af41a1a6df3a> received termination event` at 23:40:14Z | Pass |
| 71 heartbeats over ~3 h | `sending heartbeat (1/72)` → `(71/72)` at 2.5 min intervals | Pass |
| PDB blocked drain | `Cannot evict pod ... disruption budget` retries continuously | Pass |
| drain-timeout retries fire at 1 h cadence | `global timeout reached: 1h0m0s` at 00:40:15Z and 01:40:20Z | Pass |
| Heartbeat-timeout graceful delete fired at correct time | `heartbeat extended over threshold after 2h57m39s` at 02:37:53Z | Pass |
| Grace period cap enforced on DELETE | `(grace period: 900s)` — pod's 1200s capped to 900s | Pass |
| K8s event published | `publishing event: HeartbeatTimeoutGracefulTermination` | Pass |
| Final drain attempt preempted graceful wait | `global timeout reached: 1h0m0s` at 02:40:20Z (2m 27s after delete) | Observed |
| LM outcome | `setting lifecycle event as completed with result: ABANDON` after 10823.99s | ABANDON |
| Stubborn pod terminated (by ASG, not K8s SIGKILL) | `all non-daemonset pods terminated on node` at 02:45:06Z (before 02:52:53Z cap) | Pass |
| ABANDON race guarded | `event already completed by main goroutine, skipping ABANDON` | Pass |
| Post-abandon heartbeats rejected | `No active Lifecycle Action found with token ...` 02:40:42Z onward | Pass |

### Conclusion / recommendation

- The `max-termination-grace-period` cap **is** applied correctly on the DELETE call (pod's 1200s → 900s). That behavior matches the design.
- However, when `max-time-to-process` fires late in the window, the per-attempt `drain-timeout` can win the race and cause `ABANDON` before K8s gets a chance to enforce the 900s SIGKILL.
- In production with well-behaved pods (Test 5) this is a non-issue — pods exit inside their grace period and the run ends in `CONTINUE`. This failure mode is limited to **truly stubborn workloads that refuse to exit on SIGTERM**, and even then the instance still terminates (the ASG handles it after `ABANDON`), just via a different code path than the intended "graceful deletion → K8s SIGKILL → `CONTINUE`".
- Operators should be aware that for the `CONTINUE`-via-heartbeat-timeout path to succeed with a stubborn pod, the relationship should be `drain-timeout` remaining at graceful-delete time ≥ `max-termination-grace-period`. Practically, setting `max-time-to-process` such that the heartbeat-timeout fires with at least one full `drain-timeout` window left (i.e., `max-time-to-process ≤ N × drain-timeout − max-termination-grace-period`) avoids this race. With the current values (3h, 1h, 15m), the safe ceiling would be `max-time-to-process ≤ 2h45m` if we want the 900s cap to be respected by K8s rather than short-circuited by ASG `ABANDON`.

---

## Test 7: Stubborn pod under redesigned escalation flow (EXECUTED — 2026-04-28)

Re-run of the Test 6 adversarial scenario after the redesign that:

1. Moves the graceful force-delete escalation out of the heartbeat goroutine and into `drainNodeTarget` (so a single goroutine owns the termination decision and the A-vs-B race is gone).
2. Reduces `sendHeartbeat` to a pure heartbeat with a `max-time-to-process` safety cap.
3. Adds `validateServe()` enforcement that `max-time-to-process ≥ (drain-retries × drain-timeout) + max-termination-grace-period + 30s`.

### Setup (as run)
```
lifecycle manager image: custom build with redesign (cluster deployment)
cluster: ip-fmea1-e2e-usw2-k8s (per Test 6 lineage)
namespace: sandbox-sandbox-draychevcustgitprima-usw2-e2e

pod spec (identical to Test 6):
    spec:
      terminationGracePeriodSeconds: 1200
      containers:
      - name: sleeper
        command:
        - sh
        - -c
        - |
          trap 'echo "SIGTERM received at $(date) — INTENTIONALLY sleeping 1500s to exceed 900s cap"; sleep 1500; exit 0' TERM
          while true; do sleep 1; done
    # PDB: maxUnavailable: 0

container settings (lifecycle-manager):
     - --queue-name=ip-fmea1-e2e-usw2-k8s-lifecycle-manager
     - --region=us-west-2
     - --polling-interval=10
     - --drain-timeout=3600
     - --drain-timeout-unknown=60
     - --max-time-to-process=11750         # satisfies validateServe: ceiling = 3×3600+900+30 = 11730
     - --max-drain-concurrency=32
     - --max-termination-grace-period=900
     - --with-deregister=true
     - --deregister-target-types=classic-elb,target-group
     - --log-level=debug
```

### Actual outcome
| Field | Value |
|---|---|
| Instance | `i-09a78acac8bee407f` |
| Event ID | `35c6774b-fe4c-bf3f-0f84-2ef8956d6c07` |
| Wall-clock processing | **11727.1 s** (~**3 h 15 m 27 s**) from event start to completion log |
| LM result | **`CONTINUE`** (lifecycle completed successfully) |
| Representative log | `time="2026-04-28T10:46:25Z" level=info msg="event 35c6774b-fe4c-bf3f-0f84-2ef8956d6c07 for instance i-09a78acac8bee407f completed after 11727.105154335s"` |

### Actual timeline (high level)
```
T=0              Event received; PDB-blocked drain; heartbeats continue on cadence
T≈1h / 2h / 3h   Three full drain-timeout cycles (3600s each) exhaust retries
T≈3h             drainNode fails → escalateDrainFailure: force-delete with grace=900s, wait up to 930s
T≈3h15m27s       Pods gone; deregister / delete node; CompleteEvent → CONTINUE
```

### Validation matrix (actual)
| Check | Evidence | Result |
|---|---|---|
| Long run completes | completion log at ~11727 s | Pass |
| LM outcome | `CONTINUE` (not `ABANDON`) | Pass |
| Race vs Test 6 | Main path owned escalation; heartbeat did not decide termination | Pass |
| validateServe | `--max-time-to-process=11750` ≥ required ceiling 11730 | Pass |

### Conclusion

The overnight run matched the redesigned model: enough wall time for three drain attempts, then in-process graceful force-delete and wait for the capped grace window, then successful lifecycle completion. The Test 6 failure mode (drain timeout winning over the 900s K8s kill window) did not recur.
