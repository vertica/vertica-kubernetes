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

# stop the cluster and upgrade each pod with a new vertica image

set -o errexit

TIMEOUT=480  # Default, can be overridden

function usage() {
    echo "usage: $(basename $0) [options] <vdb_name> <vertica_image> "
    echo
    echo "Options:"
    echo "  -n    Namespace that we will search resources in"
    echo "  -t    Timeout in seconds. Should be an Integer.  Defaults to 360"
    echo "  -y    To skip the shutdown cluster warning"
    echo "  -p    password to use for the admintools command"
    echo "  -h    help flag"
    echo
    exit 1
}

while getopts "n:t:p:yh" opt
do
    case $opt in
        n)
            NAMESPACE=$OPTARG
            ;;
        p)
            PASSWORD=$OPTARG
            ;;
        t)
            TIMEOUT=$OPTARG
            re='^[0-9]+$'
            if ! [[ $TIMEOUT =~ $re ]] ; then
                echo "error: timeout Not a number"
                usage
            fi
            ;;
        y)
            ANSWER="y"
            ;;
        h)
            usage
            ;;
        \?) 
            echo "Unknown option: -$opt" 
            usage
            ;;
        :) 
            echo "Missing option argument for -$opt"
            usage
            ;;
        *) 
            echo "Unimplemented option: -$opt"
            usage
            ;;
    esac
done
shift "$((OPTIND-1))"

if [ "$#" -ne 2 ]; then
  echo "missing or too much arguments"
  usage
fi

VDB_NAME=$1
NEW_IMAGE=$2

NS_OPT=
if [[ -n "$NAMESPACE" ]]
then
    EXISTS=$(kubectl get namespaces | grep -w "$NAMESPACE" | wc -l)
    [ $EXISTS -ne 1 ] && echo "namespace does not exist" && exit 1
    NS_OPT="-n $NAMESPACE "
fi

PS_OPT=
[[ -n "$PASSWORD" ]] && PS_OPT="--password $PASSWORD"

SELECTOR="app.kubernetes.io/instance=$VDB_NAME"
GET_VDB="kubectl get vdb $VDB_NAME $NS_OPT"
GET_POD="kubectl get pod $NS_OPT --selector=$SELECTOR"
declare -a GET_STS=($(kubectl get sts  --selector=$SELECTOR -o jsonpath='{range .items[*]}{.metadata.name}{"\n"}{end}' $NS_OPT))
POD_NAME=$(${GET_POD} -o=jsonpath='{.items[0].metadata.name}')
VDB_DBNAME=$(${GET_VDB} -o=jsonpath='{.spec.dbName}')
TOTAL_PODS=$(${GET_POD} --no-headers | wc -l)

echo "========== UPGRADE VERTICA =========="
echo 
while :
do
    [ -z "${ANSWER}" ] && read -p "The cluster will be shutdown. Do you want to continue (y/n)?" -n 1 -r ANSWER
    echo    # (optional) move to a new line
    if [[ $ANSWER =~ ^[Yy]$ ]]
    then
        break
    else
        if [[ $ANSWER =~ ^[Nn]$ ]]
        then
            echo "upgrade aborted!"
            exit 1
        fi
    fi
done

echo
echo "====== START SHUTDOWN CLUSTER ======"
echo "disabling autoRestartVertica..."
kubectl patch vdb $VDB_NAME --type=merge -p '{"spec": {"autoRestartVertica": false, "requeueTime": 5}}' $NS_OPT 1> /dev/null
kubectl wait --for=condition=AutoRestartVertica=False vdb/$VDB_NAME --timeout="${TIMEOUT}"s $NS_OPT 1> /dev/null
echo "autoRestartVertica successfully disabled"

echo "starting stop_db..."
kubectl exec $NS_OPT $POD_NAME -- admintools -t stop_db -F -d $VDB_DBNAME $PS_OPT 1> /dev/null
echo "cluster down"

echo
echo "updating $VDB_NAME with image $NEW_IMAGE"
kubectl patch vdb $VDB_NAME --type=merge -p '{"spec": {"image": "'${NEW_IMAGE}'"}}' $NS_OPT 1> /dev/null
echo "$VDB_NAME updated"

# a statefulset always upgrades its pods from the highest to the lowest index,
# and move to the next pod only when the one just recreated has achieved a 
# ready state. AutoRestartVertica set to false prevent any pod to reach that state
# so we delete the sts to have a fresh one created with upgraded pods.
echo
echo "====== START UPDATE PODS ======"
echo "deleting old pods..."
for sts_name in "${GET_STS[@]}"
do
    kubectl delete sts $NS_OPT $sts_name --cascade=foreground 1> /dev/null
done
echo "pods deleted"

echo "waiting for pods recreation..."
c=0
while [ $c -ne $TOTAL_PODS ]
do
    c=$(kubectl wait --for=condition=Initialized=True pod  -l $SELECTOR --timeout="${TIMEOUT}"s $NS_OPT 2> /dev/null | wc -l)
done
echo "new pods created"
echo "pods successfully updated"

echo
echo "====== RESTART DB ======"
echo "enabling autoRestartVertica..."
kubectl patch vdb $VDB_NAME --type=merge -p '{"spec": {"autoRestartVertica": true, "requeueTime": 0}}' $NS_OPT 1> /dev/null
kubectl wait --for=condition=AutoRestartVertica=True vdb/$VDB_NAME --timeout="${TIMEOUT}"s $NS_OPT 1> /dev/null
echo "autoRestartVertica enabled"

echo "Restarting vertica nodes..."
kubectl wait --for=condition=Ready=True pod -l $SELECTOR --timeout="${TIMEOUT}"s $NS_OPT 1> /dev/null
echo "vertica nodes successfully restarted and up"
echo "cluster up"
