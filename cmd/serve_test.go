package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

// serveFlagGlobals captures package-level serve flags for restore between tests.
type serveFlagGlobals struct {
	localMode, region, queueName, kubectlLocalPath string
	maxDrainConcurrency                            int64
	drainTimeoutSeconds                            int
	drainRetryAttempts                             int
	maxTerminationGracePeriod                      int64
	maxTimeToProcessSeconds                        int64
}

func captureServeFlagGlobals() serveFlagGlobals {
	return serveFlagGlobals{
		localMode:                 localMode,
		region:                    region,
		queueName:                 queueName,
		kubectlLocalPath:          kubectlLocalPath,
		maxDrainConcurrency:       maxDrainConcurrency,
		drainTimeoutSeconds:       drainTimeoutSeconds,
		drainRetryAttempts:        drainRetryAttempts,
		maxTerminationGracePeriod: maxTerminationGracePeriod,
		maxTimeToProcessSeconds:   maxTimeToProcessSeconds,
	}
}

func restoreServeFlagGlobals(s serveFlagGlobals) {
	localMode = s.localMode
	region = s.region
	queueName = s.queueName
	kubectlLocalPath = s.kubectlLocalPath
	maxDrainConcurrency = s.maxDrainConcurrency
	drainTimeoutSeconds = s.drainTimeoutSeconds
	drainRetryAttempts = s.drainRetryAttempts
	maxTerminationGracePeriod = s.maxTerminationGracePeriod
	maxTimeToProcessSeconds = s.maxTimeToProcessSeconds
}

func Test_validateServeConfig_defaultsSatisfyCeiling(t *testing.T) {
	snap := captureServeFlagGlobals()
	defer restoreServeFlagGlobals(snap)

	kubectl := filepath.Join(t.TempDir(), "kubectl-bin")
	if err := os.WriteFile(kubectl, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	// Align with CLI defaults from serveCmd flags (see serve.go init).
	localMode = ""
	kubectlLocalPath = kubectl
	region = "us-west-2"
	queueName = "test-queue"
	maxDrainConcurrency = 32
	drainRetryAttempts = 3
	drainTimeoutSeconds = 300
	maxTerminationGracePeriod = 900
	maxTimeToProcessSeconds = 3600

	if err := validateServeConfig(); err != nil {
		t.Fatalf("expected default-derived ceiling to pass: %v", err)
	}
}

func Test_validateServeConfig_maxTimeBelowCeiling(t *testing.T) {
	snap := captureServeFlagGlobals()
	defer restoreServeFlagGlobals(snap)

	kubectl := filepath.Join(t.TempDir(), "kubectl-bin")
	if err := os.WriteFile(kubectl, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	localMode = ""
	kubectlLocalPath = kubectl
	region = "us-west-2"
	queueName = "q"
	maxDrainConcurrency = 32
	drainRetryAttempts = 3
	drainTimeoutSeconds = 3600
	maxTerminationGracePeriod = 900
	maxTimeToProcessSeconds = 10800

	if err := validateServeConfig(); err == nil {
		t.Fatal("expected error when max-time-to-process is below ceiling (11730s)")
	}
}

func Test_validateServeConfig_maxTimeAtExactCeiling(t *testing.T) {
	snap := captureServeFlagGlobals()
	defer restoreServeFlagGlobals(snap)

	kubectl := filepath.Join(t.TempDir(), "kubectl-bin")
	if err := os.WriteFile(kubectl, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	localMode = ""
	kubectlLocalPath = kubectl
	region = "us-west-2"
	queueName = "q"
	maxDrainConcurrency = 32
	drainRetryAttempts = 3
	drainTimeoutSeconds = 3600
	maxTerminationGracePeriod = 900
	maxTimeToProcessSeconds = 11730

	if err := validateServeConfig(); err != nil {
		t.Fatalf("expected max-time exactly at ceiling to pass: %v", err)
	}
}

func Test_validateServeConfig_missingRegion(t *testing.T) {
	snap := captureServeFlagGlobals()
	defer restoreServeFlagGlobals(snap)

	kubectl := filepath.Join(t.TempDir(), "kubectl-bin")
	if err := os.WriteFile(kubectl, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	region = ""
	queueName = "q"
	kubectlLocalPath = kubectl
	maxDrainConcurrency = 32
	maxTimeToProcessSeconds = 100000
	drainRetryAttempts = 3
	drainTimeoutSeconds = 300
	maxTerminationGracePeriod = 900

	if err := validateServeConfig(); err == nil {
		t.Fatal("expected error for empty region")
	}
}

func Test_validateServeConfig_maxDrainConcurrencyZero(t *testing.T) {
	snap := captureServeFlagGlobals()
	defer restoreServeFlagGlobals(snap)

	kubectl := filepath.Join(t.TempDir(), "kubectl-bin")
	if err := os.WriteFile(kubectl, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	region = "us-west-2"
	queueName = "q"
	kubectlLocalPath = kubectl
	maxDrainConcurrency = 0
	maxTimeToProcessSeconds = 100000
	drainRetryAttempts = 3
	drainTimeoutSeconds = 300
	maxTerminationGracePeriod = 900

	if err := validateServeConfig(); err == nil {
		t.Fatal("expected error for max-drain-concurrency < 1")
	}
}
