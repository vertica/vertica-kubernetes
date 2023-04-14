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

# A script that will create a user in a kind cluster that doesn't have full
# admin access. We use this as setup for an e2e test to verify operator install
# when we don't have cluster admin privileges. It will generate a kubeconfig
# file that can be used to authenticate as the new user.

set -o errexit
set -o pipefail

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )"
REPO_DIR=$(dirname $SCRIPT_DIR)
KUBECONFIG_OUT=config
GROUP=tenant1

source $SCRIPT_DIR/logging-utils.sh

function usage() {
    echo "usage: $0 [-o <outfile>] [-g <group>] <user>"
    echo
    echo "Optional Arguments:"
    echo " -v               Verbose output"
    echo " -o <kube-config> Output file that has the kube-config we generate."
    echo " -g <group>       Name of the group the user belongs too"
    echo
    echo "Positional Arguments:"
    echo " <user>           The name of the user to create"
    exit 1
}

while getopts "hvo:g:" opt
do
    case $opt in
      h) usage;;
      v) set -o xtrace;;
      o) KUBECONFIG_OUT=$OPTARG;;
      g) GROUP=$OPTARG;;
    esac
done

if [ $(( $# - $OPTIND )) -lt 0 ]
then
    usage
fi

USER=${@:$OPTIND:1}
logInfo "Creating a kube config stored at $KUBECONFIG_OUT for user $USER"

if [[ -d $KUBECONFIG_OUT ]]
then
    logError "kube-config output file is a directory: $KUBECONFIG_OUT"
    exit 1
fi

context_name=$(kubectl config current-context)
if ! [[ $context_name =~ ^kind-* ]]
then
    logWarning "This script can only be run against a kind cluster. Is $context_name a kind cluster?"
fi

k8s_server=$(kubectl config view --minify -o jsonpath='{.clusters[0].cluster.server}')
logInfo "Extracted k8s server address: $k8s_server"
k8s_server_port=$(echo $k8s_server | cut -d':' -f3)

control_plane=$(docker ps --filter "publish=$k8s_server_port" --format='{{.Names}}')
logInfo "Control plane docker container is $control_plane"

local_ca_crt=$(mktemp /tmp/ca.crt.XXXXXX)
local_ca_key=$(mktemp /tmp/ca.key.XXXXXX)
local_user_key=$(mktemp /tmp/user1.key.XXXXXX)
local_user_csr=$(mktemp /tmp/user1.csr.XXXXXX)
local_user_crt=$(mktemp /tmp/user1.crt.XXXXXX)
trap "rm $local_ca_crt $local_ca_key $local_user_key $local_user_csr $local_user_crt" EXIT

logInfo "Copying out the kind ca cert"
docker cp $control_plane:/etc/kubernetes/pki/ca.crt $local_ca_crt
docker cp $control_plane:/etc/kubernetes/pki/ca.key $local_ca_key

logInfo "Generating key for user"
openssl genrsa -out $local_user_key 2048
openssl req -new -key $local_user_key -out $local_user_csr -subj "/CN=$USER/O=$GROUP"
openssl x509 -req -in $local_user_csr -CA $local_ca_crt -CAkey $local_ca_key -CAcreateserial -out $local_user_crt -days 360

logInfo "Write out new kubeconfig to $KUBECONFIG_OUT"
cat <<EOF > $KUBECONFIG_OUT
apiVersion: v1
kind: Config 
clusters:
- cluster:
    certificate-authority-data: $(cat $local_ca_crt | base64 -w 0)
    server: $k8s_server
  name: kind
contexts:
- context:
    cluster: kind
    user: $USER
  name: user-$USER
current-context: user-$USER
users:
- name: $USER
  user:
    client-certificate-data: $(cat $local_user_crt | base64 -w 0)
    client-key-data: $(cat $local_user_key | base64 -w 0)
EOF
