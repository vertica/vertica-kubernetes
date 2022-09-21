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

function usage {
    echo "usage: $0 [-u] [-t <seconds>] [-c <chart>]"
    echo
    echo "Options:"
    echo "  -t <seconds>  Length of the timeout."
    echo "  -c <chart>    Override the name of the chart to use."
    echo
    exit 1
}

OPTIND=1
while getopts "ht:c:" opt; do
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
        \?)
            echo "Unknown option: -${opt}"
            usage
            ;;
    esac
done

set -o xtrace
kubectl delete namespace $HADOOP_NS || :
kubectl create namespace $HADOOP_NS

if [[ "$CHART" == "$DEFCHART" ]]
then
    helm repo add vertica-charts https://vertica.github.io/charts
    helm repo update
fi

if helm install --wait -n $HADOOP_NS $RELEASE $CHART --timeout ${TIMEOUT}s
then
    echo "✔ Success"
    exit 0
fi
set +o errexit
kubectl get pods -n $HADOOP_NS
for pod in $(kubectl get pods --no-headers -o custom-columns=':metadata.name')
do
    kubectl logs -n $HADOOP_NS $pod
done
exit 1
