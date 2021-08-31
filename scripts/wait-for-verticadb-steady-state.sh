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

# Wait for the verticadb-operator to get to a steady state.

TIMEOUT=30  # Default, can be overridden

function usage() {
    echo "usage: $(basename $0) [-n <namespace>] [-t <timeout>]"
    echo
    echo "Options:"
    echo "  -n    Namespace that we will search pods in.  Defaults to current namespace"
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

LOG_CMD="kubectl ${NS_OPT}logs -l app.kubernetes.io/name=verticadb-operator -c manager --tail -1"
WEBHOOK_FILTER="--invert-match -e 'controller-runtime.webhook.webhooks' -e 'verticadb-resource'"
timeout $TIMEOUT bash -c -- "while ! $LOG_CMD | \
    grep $WEBHOOK_FILTER | \
    tail -1 | grep --quiet '\"result\": {\"Requeue\":false,\"RequeueAfter\":0}, \"err\": null'; do sleep 1; done"

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
printf "$($LOG_CMD | grep $WEBHOOK_FILTER | tail)\n"
exit 1
