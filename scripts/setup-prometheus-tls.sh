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

# A script that create prometheus tls secret

set -o errexit
set -o pipefail

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )"
REPO_DIR=$(dirname $SCRIPT_DIR)
PROMETHEUS_DIR=${REPO_DIR}/prometheus
PROMETHEUS_NS=$(grep '^PROMETHEUS_NAMESPACE' ${REPO_DIR}/Makefile | cut -d'=' -f2)
PROMETHEUS_ADAPTER_NS=$(grep '^PROMETHEUS_ADAPTER_NAMESPACE' ${REPO_DIR}/Makefile | cut -d'=' -f2)
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

PROMETHEUS_URL=${1:-}
PROMETHEUS_RELEASE=${2:-}

if [[ -z "$PROMETHEUS_URL" || -z "$PROMETHEUS_RELEASE" ]]; then
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
logInfo "Generating self-signed certs for Prometheus TLS encryption"
mkdir -p $PROMETHEUS_DIR/certs
# ca certs
CA_KEY=$PROMETHEUS_DIR/certs/ca.key
CA_CRT=$PROMETHEUS_DIR/certs/ca.crt
# prometheus certs
TLS_KEY=$PROMETHEUS_DIR/certs/tls.key
REQ_CRT=$PROMETHEUS_DIR/certs/req.crt
TLS_CRT=$PROMETHEUS_DIR/certs/tls.crt
rm -f $CA_KEY $CA_CRT $TLS_KEY $REQ_CRT $TLS_CRT

# https://stackoverflow.com/questions/21297139/how-do-you-sign-a-certificate-signing-request-with-your-certification-authority
# Generate a self-signed certificate that will be used as the root of trust
openssl req -x509 -days 365 -newkey rsa:4096 -sha256 -nodes -keyout $CA_KEY -out $CA_CRT \
	-subj "/C=US/ST=PA/L=Pittsburgh/O=OpenText/OU=Vertica/CN=$PROMETHEUS_CN"
# Create a request (without -x509) for the certificate to be signed
openssl req -newkey rsa:4096 -nodes -keyout $TLS_KEY -out $REQ_CRT \
	-subj "/C=US/ST=PA/L=Pittsburgh/O=OpenText/OU=Vertica/CN=$PROMETHEUS_CN"
# Use SAN to Sign the requested certs with the self-signed signing root certificate
openssl x509 -req -extfile <(printf "subjectAltName=DNS:$PROMETHEUS_CN") -in $REQ_CRT -days 365 -CA $CA_CRT -CAkey $CA_KEY -CAcreateserial -out $TLS_CRT
# Put the root CA cert into the signed cert to create a CA bundle
cat $CA_CRT >> $TLS_CRT

logInfo "Creating prometheus secret"
# For Prometheus using
kubectl delete secret $TLS_SECRET -n $PROMETHEUS_NS || true
kubectl create secret generic $TLS_SECRET -n $PROMETHEUS_NS --from-file=tls.key=$TLS_KEY --from-file=tls.crt=$TLS_CRT --from-file=ca.crt=$CA_CRT
# For Prometheus adapter using
kubectl create namespace $PROMETHEUS_ADAPTER_NS || true
kubectl delete secret $TLS_SECRET -n $PROMETHEUS_ADAPTER_NS || true
kubectl create secret generic $TLS_SECRET -n $PROMETHEUS_ADAPTER_NS --from-file=tls.key=$TLS_KEY --from-file=tls.crt=$TLS_CRT --from-file=ca.crt=$CA_CRT
