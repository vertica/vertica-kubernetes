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

# Setup and cleanup a kubernetes cluster using kind - kubernetes in docker

set -e

UPLOAD_IMAGES=
TAG=latest
KUBEVER=1.21.1
IP_FAMILY=ipv4
LISTEN_ALL_INTERFACES=N
VSQL_PORT=5433
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )"
REPO_DIR=$(dirname $SCRIPT_DIR)
KIND=$REPO_DIR/bin/kind
REG_NAME='kind-registry'
REG_PORT='5000'
TERM_REGISTRY=1

while getopts "ut:k:i:ap:xr:" opt
do
    case $opt in
        u) UPLOAD_IMAGES=1;;
        t) TAG=$OPTARG;;
        k) KUBEVER=$OPTARG;;
        p) PORT=$OPTARG;;
        i) IP_FAMILY=$OPTARG;;
        a) LISTEN_ALL_INTERFACES="Y";;
        r) REG_PORT=$OPTARG;;
        x) TERM_REGISTRY=;;
    esac
done

if [ $(( $# - $OPTIND )) -lt 1 ]
then
    echo "usage: kind.sh [-uax] [-t <tag>] [-k <ver>] [-p <port>] [-i <ip-family>] [-r <port>] (init|term) <name>"
    echo
    echo "Options:"
    echo "  -u     Upload the images to kind after creating the cluster."
    echo "  -t     Tag to use for the images.  Defaults to latest."
    echo "  -k     Version of Kubernetes to deploy.  Defaults to ${KUBEVER}."
    echo "  -i     IP family of the cluster (ipv4/ipv6). Defaults to ${IP_FAMILY}."
    echo "  -a     If set, listen on all interfaces."
    echo "  -p     Open port ${VSQL_PORT} on the host.  The given port is a number in"
    echo "         the range of 30000-32767.  This option is used if you want"
    echo "         to use NodePort.  The given port is the port number you use"
    echo "         in the vdb manifest."
    echo "  -r     Use port number for the registry.  Defaults to: $REG_PORT"
    echo "  -x     When terminating kind, skip killing of the registry."
    echo
    echo "Positional Arguments:"
    echo " <name>  Name to give the cluster"
    exit 1
fi
ACTION=${@:$OPTIND:1}
CLUSTER_NAME=${@:$OPTIND+1:1}

function download_kind {
    # Download kind into repo's bin dir if not present
    make kind
    ${KIND} version
}

function create_kind_cluster {
    tmpfile=$(mktemp /tmp/kind-config-XXXXXX.yaml)
    trap "rm $tmpfile" 0 2 3 15  # Ensure deletion on script exit
    cat <<- EOF > $tmpfile
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
# Patch in the container registry
containerdConfigPatches:
- |-
  [plugins."io.containerd.grpc.v1.cri".registry.mirrors."localhost:${REG_PORT}"]
    endpoint = ["http://${REG_NAME}:${REG_PORT}"]
networking:
  ipFamily: ${IP_FAMILY}
EOF
    if [[ "$LISTEN_ALL_INTERFACES" == "Y" ]]; then
        if [[ "$IP_FAMILY" == "ipv6" ]]; then
            ADDR=0:0:0:0:0:0:0:0
        else
            ADDR=0.0.0.0
        fi
    cat <<- EOF >> $tmpfile
  apiServerAddress: $ADDR
EOF
    fi
    cat <<- EOF >> $tmpfile
nodes:
- role: control-plane
EOF
    if [[ -n "$PORT" ]]
    then
        cat <<- EOF >> $tmpfile
  extraPortMappings:
    - containerPort: $PORT
      hostPort: $VSQL_PORT
    - containerPort: $(( $PORT + 1 ))
      hostPort: $(( $VSQL_PORT + 1 ))
EOF
    fi
    cat $tmpfile

    ${KIND} create cluster --name ${CLUSTER_NAME} --image kindest/node:v${KUBEVER} --config $tmpfile --wait 5m
}

function create_registry {
    # create registry container unless it already exists
    running="$(docker inspect -f '{{.State.Running}}' "${REG_NAME}" 2>/dev/null || true)"
    if [ "${running}" != 'true' ]; then
    docker run \
        -d --restart=always -p "127.0.0.1:${REG_PORT}:5000" --name "${REG_NAME}" \
        registry:2
    fi
}

function finalize_registry {
    # connect the registry to the cluster network
    # (the network may already be connected)
    docker network connect "kind" "${REG_NAME}" || true

    # Document the local registry
    # https://github.com/kubernetes/enhancements/tree/master/keps/sig-cluster-lifecycle/generic/1755-communicating-a-local-registry
    cat <<EOF | kubectl apply -f -
apiVersion: v1
kind: ConfigMap
metadata:
  name: local-registry-hosting
  namespace: kube-public
data:
  localRegistryHosting.v1: |
    host: "localhost:${reg_port}"
    help: "https://kind.sigs.k8s.io/docs/user/local-registry/"
EOF
}

function init_kind {
    create_registry
    create_kind_cluster
    finalize_registry

    if [[ -n $UPLOAD_IMAGES ]]
    then
        $SCRIPT_DIR/push-to-kind.sh -t ${TAG} ${CLUSTER_NAME}
    fi
}

function term_kind {
    ${KIND} delete cluster --name ${CLUSTER_NAME}

    if [[ -n $TERM_REGISTRY ]]
    then
        running="$(docker inspect -f '{{.State.Running}}' "${REG_NAME}" 2>/dev/null || true)"
        if [ "${running}" == 'true' ]; then
            docker rm --force ${REG_NAME}
        fi
    fi
}

cd $REPO_DIR
download_kind
if [[ "$ACTION" == "init" ]]
then
    init_kind
elif [[ "$ACTION" == "term" ]]
then
    term_kind
else
    echo "$ACTION is not a valid action"
    exit 1
fi
