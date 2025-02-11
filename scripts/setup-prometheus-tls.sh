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

# A script that download krew locally and set it up

set -o errexit
set -o pipefail

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )"
REPO_DIR=$(dirname $SCRIPT_DIR)
PROMETHEUS_DIR=${REPO_DIR}/prometheus
PROMETHEUS_NS=$(grep '^PROMETHEUS_NAMESPACE' ${REPO_DIR}/Makefile | cut -d'=' -f2)
# The secret to secure Prometheus API endpoints using TLS encryption
# It must be the same to the secret name configured in prometheus/values-tls.yaml
TLS_SECRET=prometheus-tls

function usage {
    echo "usage: $0 <prometheus-url>"
    echo
    echo "<prometheus-url> is the Prometheus service URL"
    echo
    exit 1
}

PROMETHEUS_URL=$1
if [ -z $PROMETHEUS_URL ]; then
  usage
fi
PROMETHEUS_CN="${PROMETHEUS_URL##*/}"

set -o xtrace

source $SCRIPT_DIR/logging-utils.sh

# Generating certs and creating secret for Prometheus TLS encryption
#
# Example to use the secret in the Vertica CR:
#  spec:
#    certSecrets:
#    - name: prometheus-tls
#
# Example to access the prometheus endpoint with TLS cert:
# $ curl --cacert /certs/prometheus-tls/tls.crt https://prometheus-kube-prometheus-prometheus.prometheus.svc:9090/metrics
if ! kubectl get secret $TLS_SECRET -n $PROMETHEUS_NS &> /dev/null; then
  logInfo "Generating self-signed certs for Prometheus TLS encryption"
  mkdir -p $PROMETHEUS_DIR/certs
  TLS_KEY=$PROMETHEUS_DIR/certs/tls.key
  TLS_CRT=$PROMETHEUS_DIR/certs/tls.crt
  rm -f $TLS_KEY $TLS_CRT
  openssl req -x509 -newkey rsa:4096 -nodes -keyout $TLS_KEY -out $TLS_CRT \
         -subj "/C=US/ST=PA/L=Pittsburgh/O=OpenText/OU=Vertica/CN=$PROMETHEUS_CN"

  logInfo "Creating prometheus secret"
  kubectl create secret generic $TLS_SECRET -n $PROMETHEUS_NS --from-file=tls.key=$TLS_KEY --from-file=tls.crt=$TLS_CRT
fi
