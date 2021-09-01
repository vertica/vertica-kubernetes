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
KUBEVER=1.21.1
IP_FAMILY=ipv4
LISTEN_ALL_INTERFACES=N
VSQL_PORT=5433
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )"
REPO_DIR=$(dirname $SCRIPT_DIR)
KIND=$REPO_DIR/bin/kind

while getopts "ut:k:i:ap:" opt
do
    case $opt in
        u) UPLOAD_IMAGES=1;;
        t) TAG=$OPTARG;;
        k) KUBEVER=$OPTARG;;
        p) PORT=$OPTARG;;
        i) IP_FAMILY=$OPTARG;;
        a) LISTEN_ALL_INTERFACES="Y";;
    esac
done

if [ $(( $# - $OPTIND )) -lt 1 ]
then
    echo "usage: kind.sh [-ua] [-t <tag>] [-k <ver>] [-p <port>] [-i <ip-family>] (init|term) <name>"
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
    echo
    echo "Positional Arguments:"
    echo " <name>  Name to give the cluster"
    exit 1
fi
ACTION=${@:$OPTIND:1}
CLUSTER_NAME=${@:$OPTIND+1:1}

# Download kind into repo's bin dir if not present
cd $REPO_DIR
make kind
${KIND} version


if [[ "$ACTION" == "init" ]]
then
    tmpfile=$(mktemp /tmp/kind-config-XXXXXX.yaml)
    trap "rm $tmpfile" 0 2 3 15  # Ensure deletion on script exit
    cat <<- EOF > $tmpfile
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
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
EOF
    fi
    cat $tmpfile

    ${KIND} create cluster --name ${CLUSTER_NAME} --image kindest/node:v${KUBEVER} --config $tmpfile --wait 5m

    if [[ -n $UPLOAD_IMAGES ]]
    then
        push-to-kind.sh -t ${TAG} ${CLUSTER_NAME}
    fi
elif [[ "$ACTION" == "term" ]]
then
    ${KIND} delete cluster --name ${CLUSTER_NAME}
else
    echo "$ACTION is not a valid action"
    exit 1
fi
