package service

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	drain "k8s.io/kubectl/pkg/drain"

	"github.com/avast/retry-go"
	"github.com/keikoproj/lifecycle-manager/pkg/log"
	"k8s.io/client-go/kubernetes"
)

func getNodeByInstance(k kubernetes.Interface, instanceID string) (v1.Node, bool) {
	var foundNode v1.Node
	nodes, err := k.CoreV1().Nodes().List(context.Background(), metav1.ListOptions{})
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

func drainNode(kubeClient kubernetes.Interface, node *corev1.Node, timeout, retryInterval int64, retryAttempts uint) error {
	if timeout == 0 {
		log.Warn("skipping drain since timeout was set to 0")
		return nil
	}

	for retryAttempts > 0 {
		err := DrainNode(node, int(timeout), kubeClient)
		if err != nil {
			log.Errorf("failed to drain node %v  error: %v ", node.Name, err)
			return err
		}
		log.Info("retrying drain")
		retryAttempts -= 1
	}

	return nil
}

func runCommandWithContext(call string, args []string, timeoutSeconds, retryInterval int64, retryAttempts uint) error {
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
		retry.Attempts(retryAttempts),
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

	nodes, err := kubeClient.CoreV1().Nodes().List(context.Background(), metav1.ListOptions{})
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

// DrainNode cordons and drains a node.
func DrainNode(node *corev1.Node, DrainTimeout int, client kubernetes.Interface) error {
	if client == nil {
		return fmt.Errorf("K8sClient not set")
	}

	if node == nil {
		return fmt.Errorf("node not set")
	}

	helper := &drain.Helper{
		Client:              client,
		Force:               true,
		GracePeriodSeconds:  -1,
		IgnoreAllDaemonSets: true,
		Out:                 os.Stdout,
		ErrOut:              os.Stdout,
		DeleteEmptyDirData:  true,
		Timeout:             time.Duration(DrainTimeout) * time.Second,
	}

	if err := drain.RunCordonOrUncordon(helper, node, true); err != nil {
		if apierrors.IsNotFound(err) {
			return err
		}
		return fmt.Errorf("error cordoning node: %v", err)
	}

	if err := drain.RunNodeDrain(helper, node.Name); err != nil {
		if apierrors.IsNotFound(err) {
			return err
		}
		return fmt.Errorf("error draining node: %v", err)
	}
	return nil
}
