package service

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

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

func drainNode(kubectlPath, nodeName string, timeout int64) error {
	log.Infof("draining node %v", nodeName)
	drainArgs := []string{"drain", nodeName, "--ignore-daemonsets=true", "--delete-local-data=true", "--force", "--grace-period=-1"}
	drainCommand := kubectlPath

	_, err := runCommandWithContext(drainCommand, drainArgs, timeout)
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

func runCommandWithContext(call string, args []string, timeoutSeconds int64) (string, error) {
	// Create a new context and add a timeout to it
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeoutSeconds)*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, call, args...)
	out, err := cmd.CombinedOutput()

	if ctx.Err() == context.DeadlineExceeded {
		timeoutErr := fmt.Errorf("command execution timed out")
		log.Error(timeoutErr)
		return string(out), timeoutErr
	}

	if err != nil {
		log.Errorf("call failed with output: %s,  error: %s", string(out), err)
		return string(out), err
	}
	return string(out), nil
}
