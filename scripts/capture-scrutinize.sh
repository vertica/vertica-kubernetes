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

# A script that will collect scrutinize for each VerticaDB that has running
# pods.  This is used during the e2e test runs to collect diagnostic at the
# end. It is relying on the namespaces and their VerticaDB to be around at
# the end of run.


SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )"
REPO_DIR=$(dirname $SCRIPT_DIR)
NS=$(kubens)
HOST_OP_DIR=$REPO_DIR/int-tests-output
EXIT_ON_ERROR=

source $SCRIPT_DIR/logging-utils.sh

function usage() {
    echo "usage: $(basename $0) [-n <namespace_prefix>] [-o <dir>] [-x]"
    echo
    echo "Options:"
    echo "  -n <namespace_prefix>   Collect scrutinize only for VerticaDB that "
    echo "                          have a namespace matching this prefix."
    echo "  -o <output-dir>         Directory to store scrutinize output in."
    echo "  -x                      Return an error and exit if scrutinize fails"
    exit 1
}

while getopts "n:ho:x" opt
do
    case $opt in
        n)
            NS=$(kubens | grep "^$OPTARG")
            ;;
        o)
            HOST_OP_DIR=$OPTARG
            ;;
        x)
            EXIT_ON_ERROR=1
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

logInfo "All output saved to $HOST_OP_DIR"
mkdir -p $HOST_OP_DIR

function captureK8sObjects {
    logInfo "Save off different k8s objects"
    for obj in verticadbs pods statefulsets deployments
    do
        kubectl get $obj -A -o yaml > $HOST_OP_DIR/$obj.yaml
    done
}

function captureOLMLogs {
    logInfo "Save off the OLM operator log if deployed"
    if kubectl get ns olm 2> /dev/null
    then
        kubectl logs -n olm -l app=olm-operator --tail=-1 > $HOST_OP_DIR/olm-operator.log
    fi
}

function captureScrutinize {
    logInfo "Collect scrutinize for VerticaDB clusters"
    for ns in $NS
    do
        logInfo "Collecting scrutinize in namespace $ns"
        vdb=$(kubectl get vdb -n $ns -o jsonpath='{range .items[*]}{.metadata.name}{"\n"}{end}')
        for v in $vdb
        do
            pods=($(kubectl get pods -n $ns --selector app.kubernetes.io/instance=$v -o jsonpath='{range .items[*]}{.metadata.name},{.status.phase}{"\n"}{end}' | grep ',Running' | cut -d',' -f1))
            if [[ -z $pods ]]
            then
                logWarning "No pods running. Skip collection"
                continue
            fi
            captureScrutinizeForVDB $ns $v ${pods[0]}
        done
    done
}

function captureScrutinizeForVDB {
    ns=$1
    v=$2
    pod=$3

    logInfo "Collecting scrutinize for VerticaDB named $v"

    # Depending on the deployment we have two scrutinize methods. The
    # scrutinize standalone only works with admintools based deployments.
    # For vclusterops, we have to use 'vcluster scrutinize'.
    admintoolsDeployment=$(kubectl exec -n $ns $pod -- which admintools > /dev/null 2>&1)
    admintoolsCheck=$?
    if [[ $admintoolsCheck -eq 0 ]]
    then
        scrutinizeForAdmintools $ns $v $pod
    else
        scrutinizeForVClusterOps $ns $v $pod
    fi
}

function scrutinizeForAdmintools() {
    ns=$1
    v=$2
    pod=$3

    logInfo "Admintools deployment detected"
    POD_OP_DIR="/tmp"
    OP_FILE="$ns.$v.scrutinize.tar"
    logInfo "Running scrutinize"
    set -o xtrace
    # We only need 1 pod because scrutinize will collect for the entire VerticaDB
    kubectl exec -t -n $ns $pod -- /opt/vertica/bin/scrutinize --output_dir $POD_OP_DIR --output_file $OP_FILE --vsql-off 
    scrut_res=$?
    set +o xtrace
    if [[ -n $EXIT_ON_ERROR && $scrut_res -ne 0 ]]
    then
        logError "*** Scrutinize failed"
        exit 1
    fi
    logAndRunCommand kubectl cp -n $ns $pod:$POD_OP_DIR/$OP_FILE $HOST_OP_DIR/$OP_FILE
    set +o xtrace
}

function scrutinizeForVClusterOps() {
    ns=$1
    v=$2
    pod=$3

    logInfo "VClusterOps deployment detected"
    set -o xtrace
    mapfile -t host_list < <(kubectl get pods -n $ns --selector app.kubernetes.io/instance=$v -o jsonpath='{range .items[*]}{.metadata.name}.{.spec.subdomain}.{.metadata.namespace}.svc{"\n"}{end}')
    hosts=$(IFS=, ; echo "${host_list[*]}")
    superuser_op=$(kubectl get vdb -n $ns $v -o jsonpath='{.metadata.annotations.vertica\.com/superuser-name}')
    superuser=${superuser_op:-dbadmin}
    password_secret=$(kubectl get vdb -n $ns $v -o jsonpath='{.spec.passwordSecret}')
    if [[ -n "$password_secret" ]]
    then
        password=$(kubectl get secret -n $ns $password_secret -o jsonpath='{.data.password}' | base64 -d)
    else
        password=""
    fi
    set +o xtrace
    logInfo "Running scrutinize"
    # The output file is displayed in the output. We don't have an
    # option to control the name of the file so we must extract it from
    # the output and rename it.
    set -o xtrace
    scrut_out=$(kubectl exec -t -n $ns $pod -- /opt/vertica/bin/vcluster \
        scrutinize \
        --hosts=$hosts \
        --db-user=$superuser \
        --password=$password \
        --honor-user-input)
    set +o xtrace
    scrut_res=$?
    if [[ -n $EXIT_ON_ERROR && $scrut_res -ne 0 ]]
    then
        logError "*** Scrutinize failed"
        exit 1
    fi
    scrutinizeTmp=/tmp/scrutinize
    regex="Scrutinize final result at $scrutinizeTmp/(.+\.tar)"
    if [[ $scrut_out =~ $regex ]]
    then
        tarFile=${BASH_REMATCH[1]}
        logAndRunCommand kubectl cp -n $ns $pod:$scrutinizeTmp/$tarFile $HOST_OP_DIR/$tarFile
        mv $HOST_OP_DIR/$tarFile $HOST_OP_DIR/$ns.$v.scrutinize.tar
    else
        logError "Could not find location of scrutinize file"
        if [[ -n $EXIT_ON_ERROR ]]
        then
            exit 1
        fi
    fi
}

captureK8sObjects
captureOLMLogs
captureScrutinize
