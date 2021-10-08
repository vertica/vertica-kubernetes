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

# A script that will setup minio for use with e2e tests

set -o errexit
set -o pipefail

HDFS_NS=kuttl-e2e-hdfs
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )"
REPO_DIR=$(dirname $SCRIPT_DIR)
TIMEOUT=360
HDFS_RELEASE=hdfs
CHART=vertica-charts/hdfs-ci

function usage {
    echo "usage: $0 [-u] [-t <seconds>] [-c <chart>]"
    echo
    echo "Options:"
    echo "  -t <seconds>  Length of the timeout."
    echo "  -c <chart>    Override the name of the chart to use."
    echo "  -u            Update helm chart repository"
    echo
    exit 1
}

OPTIND=1
while getopts "ht:uc:" opt; do
    case ${opt} in
        h)
            usage
            ;;
        t)
            TIMEOUT=$OPTARG
            ;;
        u)
            UPDATE_HELM_CHART_REPO=0
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
kubectl delete namespace $HDFS_NS || :
kubectl create namespace $HDFS_NS

if [[ -n "$UPDATE_HELM_CHART_REPO" ]]
then
    helm repo add vertica-charts https://vertica.github.io/charts
    helm repo update
fi

helm install --wait -n $HDFS_NS $HDFS_RELEASE $CHART --timeout ${TIMEOUT}s -f ~/tmp/extra-conf.yaml
