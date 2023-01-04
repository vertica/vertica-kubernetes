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

# A script that will setup hadoop for use with e2e tests

set -o errexit
set -o pipefail

HADOOP_NS=kuttl-e2e-hadoop
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )"
REPO_DIR=$(dirname $SCRIPT_DIR)
TIMEOUT=900
RELEASE=hdfs-ci
CHART=vertica-charts/hdfs-ci
DEFCHART=$CHART
MAX_RETRY=5
source $SCRIPT_DIR/logging-utils.sh

function usage {
    echo "usage: $0 [-u] [-t <seconds>] [-c <chart>] [-m <num>]"
    echo
    echo "Options:"
    echo "  -t <seconds>  Length of the timeout."
    echo "  -c <chart>    Override the name of the chart to use."
    echo "  -m <num>      Maximum number of iterations to do before giving up. [Default: $MAX_RETRY]"
    echo
    exit 1
}

OPTIND=1
while getopts "ht:c:m:" opt; do
    case ${opt} in
        h)
            usage
            ;;
        t)
            TIMEOUT=$OPTARG
            ;;
        c)
            CHART=$OPTARG
            ;;
        m)
            MAX_RETRY=$OPTARG
            ;;
        \?)
            echo "Unknown option: -${opt}"
            usage
            ;;
    esac
done

if [[ "$CHART" == "$DEFCHART" ]]
then
    logInfo "Add helm chart repo"
    helm repo add vertica-charts https://vertica.github.io/charts
    helm repo update
fi

for i in $(seq 1 $MAX_RETRY)
do
    logInfo "Attempt $i to create hdfs backend"
    logInfo "Create new namespace $HADOOP_NS"
    kubectl delete namespace $HADOOP_NS || :
    kubectl create namespace $HADOOP_NS

    logInfo "Start the helm install..."
    if helm install --wait -n $HADOOP_NS $RELEASE $CHART --timeout ${TIMEOUT}s
    then
        logInfo "Helm chart successfully installed"
        exit 0
    fi
    logWarning "Timed out waiting for helm install. Dumping diagnostics."
    set +o errexit
    kubectl get pods -n $HADOOP_NS
    for pod in $(kubectl get pods --no-headers -o custom-columns=':metadata.name')
    do
        kubectl logs -n $HADOOP_NS $pod
    done
done
logError "Attempted to create hdfs helm chart. But failed each time. Failing script"
exit 1
