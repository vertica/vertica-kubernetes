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

# This script is used to update a sandbox in a VerticaDB
# with a new image name.
set -o errexit

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )"
REPO_DIR=$(dirname $SCRIPT_DIR)

function usage() {
    echo "usage: $(basename $0) [-n <namespace>] <vdb_name> [<image>]"
    echo
    echo "Options:"
    echo "  -n <namespace>  Namespace of the vdb object"
    echo
    echo "This script is used to update a sandbox in a VerticaDB"
    echo "with a new image name. If the image isn't"
    echo "specified it uses the one output from"
    echo "make echo-images."
    exit 1
}

while getopts "hn:" opt
do
    case $opt in
        h) 
            usage
            ;;
        n)
            NS_OPT="-n $OPTARG"
            ;;
        \?)
            echo "ERROR: unrecognized option: -$opt"
            usage
            ;;
    esac
done
shift "$((OPTIND-1))"

if [ "$#" -lt 1 ]; then
    echo "expecting at least 1 positional arguments"
    usage
fi

VDB_NAME=$1
SB_NAME=$2
IMAGE=$3

if [ -z "$IMAGE" ]
then
    IMAGE=$(cd $REPO_DIR && make echo-images | grep ^VERTICA_IMG= | cut -d= -f2)
fi

INDEX=$(kubectl get vdb $VDB_NAME $NS_OPT -o json |  jq ".spec.sandboxes | map(.name == \"$SB_NAME\") | index(true)")
if [[ $INDEX == "null" ]]; then
  echo "sandbox $SB_NAME not found in verticaDB $VDB_NAME"
  exit 1
fi
# Create the JSON patch dynamically
PATCH=$(cat <<EOF
[
  {
    "op": "replace",
    "path": "/spec/sandboxes/$INDEX/image",
    "value": "$IMAGE"
  }
]
EOF
)

kubectl patch vdb $VDB_NAME $NS_OPT --type="json" -p="$PATCH"
