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

# Randomly delete a vertica pod in a namespace

function usage() {
    echo "usage: $(basename $0) [-n <namespace>] [<podsToKill>]"
    echo
    echo "Options:"
    echo "  -n    Namespace that we will search pods in.  Defaults to current namespace"
    echo
    exit 1
}

while getopts "n:h" opt
do
    case $opt in
        n)
            NAMESPACE=$OPTARG
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

if [ $(( $# - $OPTIND )) -lt 0 ]
then
  PODS_TO_KILL=1
else
  PODS_TO_KILL=${@:$OPTIND:1}
fi
echo "Killing $PODS_TO_KILL pods..."

NS_OPT=
if [[ -n "$NAMESPACE" ]]
then
    NS_OPT="-n $NAMESPACE "
fi

for i in $(seq 1 $PODS_TO_KILL)
do
    mapfile -t pods  < <(kubectl ${NS_OPT}get pod -l app.kubernetes.io/name=vertica -o jsonpath='{range .items[*]}{.metadata.name}{"\n"}{end}')
    # Skip if no pods found
    if [ ${#pods[@]} -eq 0 ]
    then
        exit 0
    fi

    podIndex=$(($RANDOM % ${#pods[@]}))
    kubectl ${NS_OPT}delete pod ${pods[$podIndex]}
done
