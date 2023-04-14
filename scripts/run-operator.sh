#!/bin/bash

# (c) Copyright [2021-2023] Open Text.
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

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )"
REPO_DIR=$(dirname $SCRIPT_DIR)
INT_TEST_OUTPUT_DIR=${REPO_DIR}/int-tests-output
NS=$(kubectl config view --minify --output 'jsonpath={..namespace}' 2> /dev/null)

source $SCRIPT_DIR/logging-utils.sh

set -o errexit
set -o pipefail

# This is a script that will run the operator from your local environment. It
# won't run it in a k8s pod. This has the advantage of starting quicker, since
# we can use the Go cache to speed up the build times. The downside is that the
# webhook has to be disabled. There is no way for k8s to send webhook requests
# to your local environment.

function usage {
    echo "$0 [-l <log_dir>] [-v]"
    echo
    echo "Options:"
    echo "  -l <log_dir>        Log directory.   default: $INT_TEST_OUTPUT_DIR"
    echo "  -v                  Verbose output"
    exit 1
}

OPTIND=1
while getopts l:hv opt; do
    case ${opt} in
        l)
            INT_TEST_OUTPUT_DIR=${OPTARG}
            ;;
        h)
            usage
            ;;
        v)
            set -o xtrace
            ;;
        \?)
            echo "ERROR: unrecognized option: -$opt"
            usage
            ;;
    esac
done

function logGeneric {
    printf $1
    shift
    printf $(date "+%D")
    printf " "
    printf $(date "+%T")
    printf " [$1]  "
    shift
    for i in $*; do
        printf "$i "
    done
    printf "$NC\n"

}

function logInfo {
    logGeneric ${CYAN} "ℹ" $@
}

function logError {
    logGeneric ${RED} "!" $@
}

cd $REPO_DIR

mkdir -p $INT_TEST_OUTPUT_DIR
OP="${INT_TEST_OUTPUT_DIR}/local-verticadb-operator.log"

if helm list -o json | jq --exit-status '.[] | select(.chart|test("^verticadb-operator")) | .name'
then
    logError "verticadb-operator helm chart already installed in namespace"
    exit 1
fi

logInfo "Creating operator ConfigMap"
TMPFILE=$(mktemp /tmp/configmap.yaml.XXXXXX)

# The operator depends on the existence of a ConfigMap. We create it here.
sed 's/\(WEBHOOK_CERT_SOURCE\).*/\1: internal/;s/\(name:\) .*/\1 verticadb-operator-manager-config/;s/namespace: .*//' helm-charts/verticadb-operator/templates/verticadb-operator-manager-config-cm.yaml > $TMPFILE
kubectl apply -f $TMPFILE

trap \
    "if [ -f $TMPFILE ]; then logInfo 'Deleting operator ConfigMap'; kubectl delete -f $TMPFILE; rm $TMPFILE; fi; trap - SIGTERM && kill -- -$$ 2> /dev/null" \
    SIGINT SIGTERM ERR EXIT

# We cannot have webhooks enabled when running in this mode. With the operator
# running locally, there is no way for k8s to callout to us.
logInfo "Starting operator. Hit CTRL-C to quit."
logInfo "Watching namespace: $NS"
logInfo "Output send to: $OP"
WATCH_NAMESPACE=$NS ENABLE_WEBHOOKS=false go run cmd/operator/main.go -enable-profiler -service-account-name=default 2>&1 1> $OP &
OP_PID=$!
wait $OP_PID
