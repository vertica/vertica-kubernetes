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

# A script that will setup Operator Lifecycle Manager (OLM) for use with e2e tests

set -o errexit
set -o pipefail

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )"
REPO_DIR=$(dirname $SCRIPT_DIR)
OPERATOR_SDK=${REPO_DIR}/bin/operator-sdk
OLM_NS=olm
TIMEOUT=120
OPERATOR_NAME=verticadb-operator
CATALOG_SOURCE_NAME=$(grep OLM_TEST_CATALOG_SOURCE= $REPO_DIR/Makefile | cut -d'=' -f2)

function usage {
    echo "usage: $0 [-f] [-t <seconds>] [<catalog_source_name>]"
    echo
    echo "<catalog_source_name> is the name of the OLM catalog to "
    echo "create -- the name of the CatalogSource object.  If omitted"
    echo "this defaults to: $CATALOG_SOURCE_NAME"
    echo
    echo "Options:"
    echo "  -t <seconds>  Length of the timeout."
    echo "  -f            Force the removal of olm before beginning"
    echo
    exit 1
}

OPTIND=1
while getopts "ht:f" opt; do
    case ${opt} in
        h)
            usage
            ;;
        t)
            TIMEOUT=$OPTARG
            ;;
        f)
            FORCE_OLM_REMOVAL=1
            ;;
        \?)
            echo "Unknown option: -${opt}"
            usage
            ;;
    esac
done

if [ $(( $# - $OPTIND )) -ge 0 ]
then
    CATALOG_SOURCE_NAME=${@:$OPTIND:1}
fi

echo "Catalog source name: $CATALOG_SOURCE_NAME"

set -o xtrace

cd $REPO_DIR

# Teardown olm if '-f' option was set
if [ -n "$FORCE_OLM_REMOVAL" ]
then
    $OPERATOR_SDK olm uninstall || true
fi

# Setup olm if not already present
if ! $SCRIPT_DIR/is-openshift.sh 
then
    if ! kubectl get -n $OLM_NS deployment olm-operator
    then
        # When changing the olm version, update the digest in tests/external-images.txt
        $OPERATOR_SDK olm install --version 0.18.3

        # Delete the default catalog that OLM ships with to avoid a lot of duplicates entries.
        kubectl delete catalogsource operatorhubio-catalog -n $OLM_NS || true
    fi
else
    OLM_NS=openshift-marketplace
fi

# Create a catalog source using the catalog we build with 'docker-build-olm-catalog'
cat <<EOF | kubectl apply -f -
apiVersion: operators.coreos.com/v1alpha1
kind: CatalogSource
metadata:
  name: $CATALOG_SOURCE_NAME
  namespace: $OLM_NS
spec:
  sourceType: grpc
  image: $(make echo-images | grep OLM_CATALOG_IMG | cut -d"=" -f2)
EOF

# Wait for the catalog source to be ready
set +o xtrace
echo "Waiting for catalog source to be ready..."
trap "echo 'Failed waiting for catalog source to be ready'; kubectl get catalogsource -n $OLM_NS $CATALOG_SOURCE_NAME -o yaml" 0 2 3 15
timeout $TIMEOUT bash -c -- "\
    while ! kubectl get catalogsource -n $OLM_NS $CATALOG_SOURCE_NAME -o jsonpath='{.status.connectionState.lastObservedState}' |  grep -cq 'READY'; \
    do \
        sleep 0.1; \
    done" &
pid=$!
wait $pid
trap - 0 2 3 15 1> /dev/null
set -o xtrace

# Wait for the operator to show up in the manifest
set +o xtrace
echo "Waiting for operator to show up in the package manifest..."
trap "echo 'Failed waiting for operator to appear in the package manifest'; kubectl get packagemanifests -n $OLM_NS" 0 2 3 15
timeout $TIMEOUT bash -c -- "\
    while ! kubectl get packagemanifests -n $OLM_NS verticadb-operator 2> /dev/null; \
    do \
        sleep 0.1; \
    done" &
pid=$!
wait $pid
trap - 0 2 3 15 1> /dev/null
set -o xtrace
