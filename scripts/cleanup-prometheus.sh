#!/bin/bash

# (c) Copyright [2021-2024] Open Text.
# Licensed under the Apache License, Version 2.0 (the "License");
# You may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
# http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

# A script that will clean up left over resources of Prometheus.

NAMESPACE=$1
kubectl delete clusterrole prometheus-kube-prometheus-operator -n $NAMESPACE
kubectl delete clusterrole prometheus-kube-prometheus-prometheus -n $NAMESPACE
kubectl delete clusterrolebinding prometheus-kube-prometheus-operator -n $NAMESPACE
kubectl delete clusterrolebinding prometheus-kube-prometheus-prometheus -n $NAMESPACE
kubectl delete svc prometheus-kube-prometheus-kube-proxy -n kube-system
kubectl delete svc prometheus-kube-prometheus-kubelet -n kube-system
kubectl delete MutatingWebhookConfiguration prometheus-kube-prometheus-admission -n $NAMESPACE
kubectl delete ValidatingWebhookConfiguration prometheus-kube-prometheus-admission -n $NAMESPACE
