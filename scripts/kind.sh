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

# Setup and cleanup a kubernetes cluster using kind - kubernetes in docker

set -e

UPLOAD_IMAGES=
TAG=latest
if [[ -n $KUBECONFIG ]]
then
    KUBECONFIG=$HOME/.kube/config
fi
KUBEVER=1.19.3

while getopts "ut:k:" opt
do
    case $opt in
        u) UPLOAD_IMAGES=1;;
        t) TAG=$OPTARG;;
        k) KUBEVER=$OPTARG;;
    esac
done

if [ $(( $# - $OPTIND )) -lt 1 ]
then
    echo "usage: kind.sh [-u] [-t <tag>] [-k <ver>] (init|term) <name>"
    echo
    echo "Options:"
    echo "  -u     Upload the images to kind after creating the cluster"
    echo "  -t     Tag to use for the images.  Defaults to latest"
    echo "  -k     Version of kubernetes to deploy.  Defaults to ${KUBEVER}."
    echo
    echo "Positional Arguments:"
    echo " <name>  Name to give the cluster"
    exit 1
fi
ACTION=${@:$OPTIND:1}
CLUSTER_NAME=${@:$OPTIND+1:1}

if [[ -z $GOPATH ]]
then
    GOPATH=$HOME
fi
PATH=$GOPATH/go/bin:$PATH
# Get kind if not present
if ! which kind > /dev/null 2>&1
then
    PWD=$(pwd)
    cd $HOME
    GO111MODULE="on" go get sigs.k8s.io/kind@v0.10.0
    cd $PWD
fi

if [[ "$ACTION" == "init" ]]
then
    tmpfile=$(mktemp /tmp/kind-config-XXXXXX.yaml)
    trap "rm $tmpfile" 0 2 3 15  # Ensure deletion on script exit
    cat <<- EOF > $tmpfile
# three node (two workers) cluster config
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
networking:
  apiServerAddress: 0.0.0.0
nodes:
- role: control-plane
- role: worker
EOF
    kind create cluster --name ${CLUSTER_NAME} --image kindest/node:v${KUBEVER} --config $tmpfile --wait 5m

    if [[ -n $UPLOAD_IMAGES ]]
    then
        push-to-kind.sh -t ${TAG} ${CLUSTER_NAME}
    fi
elif [[ "$ACTION" == "term" ]]
then
    kind delete cluster --name ${CLUSTER_NAME}
else
    echo "$ACTION is not a valid action"
    exit 1
fi
