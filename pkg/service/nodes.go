package service

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/avast/retry-go"
	"github.com/keikoproj/lifecycle-manager/pkg/log"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

func getNodeByInstance(k kubernetes.Interface, instanceID string) (v1.Node, bool) {
	var foundNode v1.Node
	nodes, err := k.CoreV1().Nodes().List(metav1.ListOptions{})
	if err != nil {
		log.Errorf("failed to list nodes: %v", err)
		return foundNode, false
	}

	for _, node := range nodes.Items {
		providerID := node.Spec.ProviderID
		splitProviderID := strings.Split(providerID, "/")
		foundID := splitProviderID[len(splitProviderID)-1]

		if instanceID == foundID {
			return node, true
		}
	}

	return foundNode, false
}

func drainNode(kubectlPath, nodeName string, timeout, retryInterval int64) error {
	log.Infof("draining node %v", nodeName)

	drainArgs := []string{"drain", nodeName, "--ignore-daemonsets=true", "--delete-local-data=true", "--force", "--grace-period=-1"}
	drainCommand := kubectlPath

	err := runCommandWithContext(drainCommand, drainArgs, timeout, retryInterval)
	if err != nil {
		if err.Error() == "command execution timed out" {
			log.Warnf("failed to drain node %v, drain command timed-out", nodeName)
			return err
		}
		log.Warnf("failed to drain node: %v", err)
		return err
	}
	return nil
}

func runCommandWithContext(call string, args []string, timeoutSeconds, retryInterval int64) error {
	// Create a new context and add a timeout to it
	timeoutErr := fmt.Errorf("command execution timed out")
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeoutSeconds)*time.Second)
	defer cancel()
	err := retry.Do(
		func() error {
			cmd := exec.CommandContext(ctx, call, args...)
			out, err := cmd.CombinedOutput()
			if ctx.Err() == context.DeadlineExceeded {
				log.Error(timeoutErr)
				return timeoutErr
			}
			if err != nil {
				log.Errorf("call failed with output: %s,  error: %s", string(out), err)
				return err
			}
			return nil
		},
		retry.RetryIf(func(err error) bool {
			if err.Error() == timeoutErr.Error() {
				return false
			}
			log.Infoln("retrying drain")
			return true
		}),
		retry.Attempts(3),
		retry.Delay(time.Duration(retryInterval)*time.Second),
	)
	if err != nil {
		return err
	}

	return nil
}
