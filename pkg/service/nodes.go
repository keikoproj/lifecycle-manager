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

func isNodeStatusInCondition(node v1.Node, condition v1.ConditionStatus) bool {
	var (
		conditions = node.Status.Conditions
	)
	for _, c := range conditions {
		if c.Type == v1.NodeReady {
			if c.Status == condition {
				return true
			}
		}
	}
	return false
}

func drainNode(kubectlPath, nodeName string, timeout, retryInterval int64) error {
	drainArgs := []string{"drain", nodeName, "--ignore-daemonsets=true", "--delete-local-data=true", "--force", "--grace-period=-1"}
	drainCommand := kubectlPath

	if timeout == 0 {
		log.Warn("skipping drain since timeout was set to 0")
		return nil
	}

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
			_, err := cmd.CombinedOutput()
			if err != nil {
				return err
			}
			return nil
		},
		retry.RetryIf(func(err error) bool {
			if err != nil {
				log.Infoln("retrying drain")
				return true
			}
			return false
		}),
		retry.Attempts(3),
		retry.Delay(time.Duration(retryInterval)*time.Second),
	)
	if err != nil {
		return err
	}

	return nil
}

func runCommand(call string, arg []string) (string, error) {
	log.Debugf("invoking >> %s %s", call, arg)
	out, err := exec.Command(call, arg...).CombinedOutput()
	if err != nil {
		log.Errorf("call failed with output: %s,  error: %s", string(out), err)
		return string(out), err
	}
	log.Debugf("call succeeded with output: %s", string(out))
	return string(out), err
}

func labelNode(kubectlPath, nodeName, labelKey, labelValue string) error {
	label := fmt.Sprintf("%v=%v", labelKey, labelValue)
	labelArgs := []string{"label", "--overwrite", "node", nodeName, label}
	_, err := runCommand(kubectlPath, labelArgs)
	if err != nil {
		log.Errorf("failed to label node %v", nodeName)
		return err
	}
	return nil
}

func annotateNode(kubectlPath string, nodeName string, annotations map[string]string) error {
	annotateArgs := []string{"annotate", "--overwrite", "node", nodeName}
	for k, v := range annotations {
		annotateArgs = append(annotateArgs, fmt.Sprintf("%v=%v", k, v))
	}
	_, err := runCommand(kubectlPath, annotateArgs)
	if err != nil {
		log.Errorf("failed to annotate node %v", nodeName)
		return err
	}
	return nil
}

func getNodesByAnnotationKeys(kubeClient kubernetes.Interface, keys ...string) (map[string]map[string]string, error) {
	results := make(map[string]map[string]string, 0)

	nodes, err := kubeClient.CoreV1().Nodes().List(metav1.ListOptions{})
	if err != nil {
		return results, err
	}

	for _, node := range nodes.Items {
		annotations := node.GetAnnotations()
		resultValues := make(map[string]string)
		for _, k := range keys {
			if v, ok := annotations[k]; ok {
				resultValues[k] = v
			}
		}
		if len(resultValues) > 0 {
			results[node.Name] = resultValues
		}
	}
	return results, nil
}
