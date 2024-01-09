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

# This script will patch the given vdb.  It expects the patch to fail.  A regex
# is given that must match the error message.  If the patch fails with the
# expected message, then the script exits with a return code of 0.

set -o errexit

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )"
REPO_DIR=$(dirname $SCRIPT_DIR)

function usage() {
    echo "usage: $(basename $0) [-n <namespace>] <vdb_name> [<image>]"
    echo
    echo "Options:"
    echo "  -n <namespace>  Namespace of the vdb object"
    echo
    echo "This script is used to patch a VerticaDB"
    echo "with a new image name.  If the image isn't"
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
IMAGE=$2

if [ -z "$IMAGE" ]
then
    IMAGE=$(cd $REPO_DIR && make echo-images | grep ^VERTICA_IMG= | cut -d= -f2)
fi

tmpfile=$(mktemp /tmp/patch-XXXXXX.yaml)
trap "rm $tmpfile" 0 2 3 15   # Ensure deletion on script exit
cat <<- EOF > $tmpfile
spec:
  image: $IMAGE
EOF

echo "Patch $VDB_NAME with image $IMAGE"
kubectl patch vdb $VDB_NAME $NS_OPT --type=merge --patch-file $tmpfile
