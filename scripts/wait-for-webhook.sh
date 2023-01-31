#!/bin/bash

# (c) Copyright [2021-2022] Micro Focus or one of its affiliates.
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

# A script that will wait for the webhook to be fully setup.  There is a small
# timing window where the pod with the webhook is up and ready, but the webhook
# is not yet able to accept connections.  See this issue for more details:
# https://github.com/vertica/vertica-kubernetes/issues/30

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )"
REPO_DIR=$(dirname $SCRIPT_DIR)
TIMEOUT=30
source $SCRIPT_DIR/logging-utils.sh

function usage() {
    echo "usage: $(basename $0) [-n <namespace>] [-t <seconds>]"
    echo
    echo "Options:"
    echo "  -n <namespace>  Check the webhook in this namespace."
    echo "  -t <seconds>    Specify the timeout in seconds [defaults: $TIMEOUT]"
    exit 1
}

while getopts "n:t:h" opt
do
    case $opt in
        n)
            NAMESPACE_OPT="-n $OPTARG"
            ;;
        t)
            TIMEOUT=$OPTARG
            ;;
        h) 
            usage
            ;;
        \?)
            echo "ERROR: unrecognized option: -$opt"
            usage
            ;;
    esac
done

logInfo "Wait for operator Deployment object to exist"
timeout $TIMEOUT bash -c -- "\
    while ! kubectl get $NAMESPACE_OPT deployments -l control-plane=controller-manager 2> /dev/null; \
    do \
      sleep 0.1; \
    done"

logInfo "Ensure that webhook is enabled for the operator"
WEBHOOK_ENABLED=$(kubectl get $NAMESPACE_OPT deployments -l control-plane=controller-manager -o jsonpath='{.items[0].spec.template.spec.containers[0].env[1].value}')
if [ "$WEBHOOK_ENABLED" == "false" ]
then
  logWarning "Webhook is not enabled. Skipping wait."
  exit 0
fi

logInfo "Ensure the service object for the webhook exists"
trap "logError 'Failed waiting for webhook service object to exist'" 0 2 3 15
set -o errexit
timeout $TIMEOUT bash -c -- "\
    while ! kubectl get $NAMESPACE_OPT svc --no-headers --selector vertica.com/svc-type=webhook 2> /dev/null | grep -cq 'service'; \
    do \
      sleep 0.1; \
    done"
set +o errexit
trap 1> /dev/null

# Next, to validate the webhook exists, we will continually create/delete a
# VerticaDB.  If it succeeds, then we assume the webhook is up and running.
# This depends on the webhook config having the 'failurePolicy: Fail' set.
logInfo "Continually create/delete a VerticaDB to verify webhook"

SELECTOR_KEY=vertica.com/use
SELECTOR_VAL=wait-for-webhook
SELECTOR=$SELECTOR_KEY=$SELECTOR_VAL

MANIFEST=$(mktemp)

cat <<EOF > $MANIFEST
apiVersion: vertica.com/v1beta1
kind: VerticaDB
metadata:
  generateName: wait-for-webhook-
  labels:
    $SELECTOR_KEY: $SELECTOR_VAL
spec:
  image: "vertica/vertica-k8s:latest"
  initPolicy: ScheduleOnly
  subclusters:
  - name: sc1
    size: 1
EOF

# Delete old manifests, but likely won't be there so eat the error.
kubectl delete $NAMESPACE_OPT vdb -l $SELECTOR 2> /dev/null 1> /dev/null || :

trap "if [ "$?" -ne 0 ]; then logError 'Timed out waiting for webhook'; fi && kubectl delete $NAMESPACE_OPT vdb -l $SELECTOR; rm $MANIFEST" 0 2 3 15   # Ensure deletion on script exit"

timeout $TIMEOUT bash -c -- "\
    while ! kubectl create $NAMESPACE_OPT -f $MANIFEST 2> /dev/null; \
    do \
      sleep 0.1; \
    done" &
pid=$!
wait $pid
logInfo "Webhook verified successfully"
