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

if ! kubectl get secret prometheus-tls -n $PROMETHEUS_NS &> /dev/null; then
  logInfo "Generating key for prometheus"
  TLS_KEY=$PROMETHEUS_DIR/certs/tls.key
  TLS_CRT=$PROMETHEUS_DIR/certs/tls.crt
  rm -f $TLS_KEY $TLS_CRT
  mkdir -p $PROMETHEUS_DIR/certs
  openssl req -x509 -newkey rsa:4096 -nodes -keyout $TLS_KEY -out $TLS_CRT \
         -subj "/C=US/ST=PA/L=Pittsburgh/O=OpenText/OU=Vertica/CN=$PROMETHEUS_CN"

  logInfo "Creating prometheus secret"
  kubectl create secret generic prometheus-tls -n $PROMETHEUS_NS --from-file=tls.key=$TLS_KEY --from-file=tls.crt=$TLS_CRT
fi

if ! grep prometheus-tls $PROMETHEUS_DIR/values.yaml &> /dev/null; then
  logInfo "Enalbe TLS in prometheus in kube-prometheus-stack values.yaml"
  cat <<EOF >> $PROMETHEUS_DIR/values.yaml

  # enable tls config
  # https://prometheus.io/docs/guides/tls-encryption/
  prometheusSpec:
    web:
      tlsConfig:
        keySecret:
          key: tls.key
          name: prometheus-tls
        cert:
          secret:
            key: tls.crt
            name: prometheus-tls
EOF
fi
