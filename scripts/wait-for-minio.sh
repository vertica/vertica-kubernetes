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

# Wait for the minio tenant to be fully setup

TIMEOUT=180  # Default, can be overridden
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )"

function usage() {
    echo "usage: $(basename $0) [-n <namespace>] [-t <timeout>]"
    echo
    echo "Options:"
    echo "  -n    Namespace that we will search the tenant in.  Defaults to current namespace"
    echo "  -t    Timeout in seconds.  Defaults to $TIMEOUT"
    echo
    exit 1
}

while getopts "n:ht:" opt
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
            echo "ERROR: unrecognized option: $opt"
            usage
            ;;
    esac
done

NS_OPT=
if [[ -n "$NAMESPACE" ]]
then
    NS_OPT="-n $NAMESPACE "
fi

kubectl kuttl assert $NS_OPT --timeout $TIMEOUT $SCRIPT_DIR/../tests/manifests/minio/assert.yaml
echo "Minio is setup for use"