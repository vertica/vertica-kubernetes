/*
 (c) Copyright [2021-2024] Open Text.
 Licensed under the Apache License, Version 2.0 (the "License");
 You may not use this file except in compliance with the License.
 You may obtain a copy of the License at

 http://www.apache.org/licenses/LICENSE-2.0

 Unless required by applicable law or agreed to in writing, software
 distributed under the License is distributed on an "AS IS" BASIS,
 WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 See the License for the specific language governing permissions and
 limitations under the License.
*/

package secrets

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// StandardK8sClient will fulfill the GetSecret API using the standard k8s
// client. It assumes it is run inside of a k8s cluster (e.g. a pod). This
// should work in most cases, but can be replaced with your own client (e.g.
// using the client from operator-sdk that has caching).
type StandardK8sClient struct {
	Clientset *kubernetes.Clientset
	Config    *rest.Config
}

// CreateSecret will create the given secret. It wil return the created secret
// and any error that may have occurred.
func (s *StandardK8sClient) CreateSecret(ctx context.Context, name types.NamespacedName, secret *corev1.Secret) (*corev1.Secret, error) {
	if err := s.loadClientSet(); err != nil {
		return nil, err
	}
	secretsClient := s.Clientset.CoreV1().Secrets(name.Namespace)
	return secretsClient.Create(ctx, secret, metav1.CreateOptions{})
}

// DeleteSecret will delete the given secret and return any error
func (s *StandardK8sClient) DeleteSecret(ctx context.Context, name types.NamespacedName) error {
	if err := s.loadClientSet(); err != nil {
		return err
	}
	secretsClient := s.Clientset.CoreV1().Secrets(name.Namespace)
	return secretsClient.Delete(ctx, name.Name, metav1.DeleteOptions{})
}

// GetSecret will read the secret using from k8s.
func (s *StandardK8sClient) GetSecret(ctx context.Context, name types.NamespacedName) (*corev1.Secret, error) {
	if err := s.loadClientSet(); err != nil {
		return nil, err
	}
	secretsClient := s.Clientset.CoreV1().Secrets(name.Namespace)
	return secretsClient.Get(ctx, name.Name, metav1.GetOptions{})
}

func (s *StandardK8sClient) loadClientSet() error {
	if s.Clientset != nil {
		return nil
	}

	var err error
	if s.Config == nil {
		s.Config, err = rest.InClusterConfig()
		if err != nil {
			return err
		}
	}
	s.Clientset, err = kubernetes.NewForConfig(s.Config)
	return err
}

func (s *StandardK8sClient) createNamespace(ctx context.Context, name string) (*corev1.Namespace, error) {
	if err := s.loadClientSet(); err != nil {
		return nil, err
	}
	nmClient := s.Clientset.CoreV1().Namespaces()
	namespace := corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	}
	return nmClient.Create(ctx, &namespace, metav1.CreateOptions{})
}

func (s *StandardK8sClient) deleteNamespace(ctx context.Context, name string) error {
	if err := s.loadClientSet(); err != nil {
		return err
	}
	nmClient := s.Clientset.CoreV1().Namespaces()
	return nmClient.Delete(ctx, name, metav1.DeleteOptions{})
}
