/*
Copyright 2021 Intuit Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package service

import (
	"fmt"
	"os"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/kubernetes"
	drain "k8s.io/kubectl/pkg/drain"
)

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
