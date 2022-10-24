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

# Wait for the revision in a StatefulsSet to be updated

TIMEOUT=30  # Default, can be overridden

function usage() {
    echo "usage: $(basename $0) [-n <namespace>] [-t <timeout>] <sts-name>"
    echo
    echo "Options:"
    echo "  -n    Namespace that we will search sts in.  Defaults to current namespace"
    echo "  -t    Timeout in seconds.  Defaults to $TIMEOUT"
    echo
    exit 1
}

while getopts "n:ht:" opt
do
    case $opt in
        n)
            NAMESPACE_OPT="-n $OPTARG"
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
shift "$((OPTIND-1))"

if [ "$#" -ne 1 ]; then
    echo "expecting exactly 1 positional argument: <sts-name>"
    usage
fi
STS_NAME=$1

crfile=$(mktemp /tmp/sts-check-current-revision-XXXXXX.txt)
urfile=$(mktemp /tmp/sts-check-update-revision-XXXXXX.txt)
trap "rm $crfile; rm $urfile" 0 2 3 15

timeout $TIMEOUT bash -c -- "while sleep 1; do kubectl $NAMESPACE_OPT get sts $STS_NAME -o=jsonpath='{.status.currentRevision}' > $crfile; kubectl $NAMESPACE_OPT get sts $STS_NAME -o=jsonpath='{.status.updateRevision}' > $urfile; if ! diff $crfile $urfile > /dev/null; then exit 0; fi; done" &
pid=$!
wait $pid

if [[ "$?" -eq 0 ]]
then
  exit 0
fi

RED='\033[0;31m'
GREEN='\033[0;32m'
NC='\033[0m'  # No color
printf "\n${RED}Timed out waiting for revision to change in statefulset\n"
printf "\n${GREEN}Status:${NC}\n"
printf "\n$(kubectl $NAMESPACE_OPT get sts $STS_NAME -o=jsonpath='{.status}')\n"
exit 1
