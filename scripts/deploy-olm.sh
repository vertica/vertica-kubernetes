#!/bin/bash

# (c) Copyright [2021] Micro Focus or one of its affiliates.
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

# A script that will deploy the operator using OLM.  It assumes OLM is setup.
# The operator will be deployed in the current namespace or the one given on
# the command line.

set -o errexit
set -o pipefail
set -o xtrace

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )"
REPO_DIR=$(dirname $SCRIPT_DIR)
TIMEOUT=120
NAMESPACE=$(kubectl config view --minify --output 'jsonpath={..namespace}')
CATALOG_SOURCE_NAME=$(grep OLM_TEST_CATALOG_SOURCE= $REPO_DIR/Makefile | cut -d'=' -f2)

function usage() {
    echo "usage: $(basename $0) [-n <namespace>] [-t <seconds>] [<catalog_source_name>]"
    echo
    echo "<catalog_source_name> is the name of the OLM catalog to use.  This was "
    echo "previously created in setup-olm.sh.  If omitted this defaults to: "
    echo $CATALOG_SOURCE_NAME
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
            NAMESPACE=$OPTARG
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

if [ $(( $# - $OPTIND )) -ge 0 ]
then
  CATALOG_SOURCE_NAME=${@:$OPTIND:1}
fi

if [ -z "$NAMESPACE" ]
then
  NAMESPACE=default
fi

echo "Namespace: $NAMESPACE"
echo "Catalog source name: $CATALOG_SOURCE_NAME"

set -o xtrace

# Create an operator group for the target namespace
cat <<EOF | kubectl apply -f -
apiVersion: operators.coreos.com/v1alpha2
kind: OperatorGroup
metadata:
  name: e2e-operatorgroup
  namespace: $NAMESPACE
spec:
  targetNamespaces:
  - $NAMESPACE
EOF

# Create a subscription to the verticadb-operator
cat <<EOF | kubectl apply -f -
apiVersion: operators.coreos.com/v1alpha1
kind: Subscription
metadata:
  name: e2e-verticadb-subscription
  namespace: $NAMESPACE
spec:
  channel: stable
  name: verticadb-operator
  source: $CATALOG_SOURCE_NAME
  sourceNamespace: olm
EOF

# Wait for the CSV to show up and report success
set +o xtrace
echo "Waiting for ClusterServicesVersion to show up..."
trap "echo 'Failed waiting for CSV to succeed'; kubectl get clusterserviceversion -n $NAMESPACE" 0 2 3 15
timeout $TIMEOUT bash -c -- "\
    while ! kubectl get -n $NAMESPACE clusterserviceversion --selector operators.coreos.com/verticadb-operator.$NAMESPACE="" 2> /dev/null | grep -cq 'Succeeded'; \
    do \
        sleep 0.1; \
    done" &
pid=$!
wait $pid
trap - 0 2 3 15 1> /dev/null
set -o xtrace
kubectl get clusterserviceversion -n $NAMESPACE