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

# A script that will collect scrutinize for each VerticaDB that has running
# pods.  This is used during the e2e test runs to collect diagnostic at the
# end. It is relying on the namespaces and their VerticaDB to be around at
# the end of run.


SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )"
REPO_DIR=$(dirname $SCRIPT_DIR)
NS=$(kubens)
HOST_OP_DIR=$REPO_DIR/int-tests-output

function usage() {
    echo "usage: $(basename $0) [-n <namespace_prefix>]"
    echo
    echo "Options:"
    echo "  -n <namespace_prefix>   Collect scrutinize only for VerticaDB that "
    echo "                          have a namespace matching this prefix."
    exit 1
}

while getopts "n:h" opt
do
    case $opt in
        n)
            NS=$(kubens | grep "^$OPTARG")
            ;;
        h) 
            usage
            ;;
        \?)
            echo "ERROR: unrecognized option: -$opt"
            usage
            ;;
    esac
done

mkdir -p $HOST_OP_DIR
for ns in $NS
do
    vdb=$(kubectl get vdb -n $ns -o jsonpath='{range .items[*]}{.metadata.name}{"\n"}{end}')
    for v in $vdb
    do
        # We only need 1 pod because scrutinize will collect for the entire VerticaDB
        pods=($(kubectl get pods -n $ns --selector app.kubernetes.io/instance=$v -o jsonpath='{range .items[*]}{.metadata.name},{.status.phase}{"\n"}{end}' | grep ',Running' | cut -d',' -f1))
        if [[ -n $pods ]]
        then
            set -o xtrace
            POD_OP_DIR="/tmp"
            OP_FILE="$ns.$v.scrutinize.tar"
            kubectl exec -t -n $ns ${pods[0]} -- /opt/vertica/bin/scrutinize --output_dir $POD_OP_DIR --output_file $OP_FILE --vsql-off 
            kubectl cp -n $ns ${pods[0]}:$POD_OP_DIR/$OP_FILE $HOST_OP_DIR/$OP_FILE
            set +o xtrace
        fi
    done
done

# Save off different k8s objects
for obj in verticadbs pods statefulsets deployments
do
    kubectl get $obj -A -o yaml > $HOST_OP_DIR/$obj.yaml
done
