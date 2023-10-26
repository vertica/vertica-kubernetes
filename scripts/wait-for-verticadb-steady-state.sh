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

# Wait for the verticadb-operator to get to a steady state. Since the operator
# is cluster scoped you need to provide the namespace of the operator and the
# namespace of the vdb you want to check.

TIMEOUT=30  # Default, can be overridden

function usage() {
    echo "usage: $(basename $0) [-n <namespace>] [-t <timeout>] [<vdb-namespace>]"
    echo
    echo "Options:"
    echo "  -n    Namespace the operator is deployed in.  Defaults to current namespace"
    echo "  -t    Timeout in seconds.  Defaults to $TIMEOUT"
    echo
    exit 1
}

OPTIND=1
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

if [ $(( $# - $OPTIND )) -eq 0 ]
then
    # All entries will have a key/value like this:
    # "verticadb": "kuttl-test-sterling-coyote/v-auto-restart",
    # We are going to look for the namespace portion.
    VDB_FILTER="${@:$OPTIND:1}/"
else
    # No verticadb namespace, so include everything in the vdb filter
    VDB_FILTER="."
fi


NS_OPT="-n verticadb-operator "
if [[ -n "$NAMESPACE" ]]
then
    NS_OPT="-n $NAMESPACE "
fi

LOG_CMD="kubectl ${NS_OPT}logs -l control-plane=controller-manager -c manager --tail=-1"
WEBHOOK_FILTER="--invert-match -e 'controller-runtime.webhook.webhooks' -e 'verticadb-resource'"
DEPRECATION_FILTER="--invert-match 'VerticaDB is deprecated'"
# Messages from AdapterPool can show up periodically. Temporarily filtering
# those out while we hunt down the source of them (see VER-89861).
ADAPTER_POOL_FILTER="--invert-match 'AdapterPool'"
timeout $TIMEOUT bash -c -- "while ! $LOG_CMD | \
    grep $WEBHOOK_FILTER | \
    grep $DEPRECATION_FILTER | \
    grep $ADAPTER_POOL_FILTER | \
    grep $VDB_FILTER | \
    tail -1 | grep --quiet '\"result\": {\"Requeue\":false,\"RequeueAfter\":0}, \"err\": null'; do sleep 1; done" &
pid=$!
wait $pid

if [[ "$?" -eq 0 ]]
then
  exit 0
fi

RED='\033[0;31m'
GREEN='\033[0;32m'
NC='\033[0m'  # No color
printf "\n${RED}Timed out waiting for steady state to be achieved.\n"
printf "\n${GREEN}Command:"
printf "\n\t$LOG_CMD\n\n${NC}"
$LOG_CMD | grep $WEBHOOK_FILTER | grep $VDB_FILTER
exit 1
