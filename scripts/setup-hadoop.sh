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

# A script that will setup hadoop for use with e2e tests

set -o errexit
set -o pipefail

HADOOP_NS=kuttl-e2e-hadoop
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )"
REPO_DIR=$(dirname $SCRIPT_DIR)
TIMEOUT=600
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

# SPILLY temp temp
cd $HOME/git/kubernetes-HDFS/charts
helm dependency build hdfs-ci
helm package hdfs-ci

helm install -n $HADOOP_NS $RELEASE $HOME/git/kubernetes-HDFS/charts/hdfs-ci-0.1.1.tgz --set global.kerberosEnabled=true --set tags.kerberos=true --timeout ${TIMEOUT}s

while ! kubectl get pod -n $HADOOP_NS hdfs-ci-krb5-0; do sleep 0.1; done
kubectl wait -n $HADOOP_NS --for=condition=Ready=True pod hdfs-ci-krb5-0 --timeout ${TIMEOUT}s

KRB5_CONFIG=hdfs-ci-krb5-config
kubectl cp -n $HADOOP_NS hdfs-ci-krb5-0:/etc/krb5.conf $HOME/tmp/krb5.conf
kubectl delete configmap -n $HADOOP_NS $KRB5_CONFIG || :
kubectl create configmap -n $HADOOP_NS $KRB5_CONFIG --from-file=$HOME/tmp/krb5.conf

HOSTS="matt1-control-plane hdfs-ci-namenode-0.hdfs-ci-namenode.kuttl-e2e-hadoop.svc.cluster.local "
for i in $(seq 0 2)
do
    HOSTS+="hdfs-ci-datanode-$i.hdfs-ci-datanode.kuttl-e2e-hadoop.svc.cluster.local "
done

ALL_PRINCIPALS=
for HOST in $HOSTS
do
    for p in hdfs HTTP
    do
      kubectl exec -n $HADOOP_NS hdfs-ci-krb5-0 -- kadmin.local -q "addprinc -randkey $p/$HOST@MYCOMPANY.COM"
      ALL_PRINCIPALS+="$p/$HOST@MYCOMPANY.COM "
    done
done

kubectl exec -n $HADOOP_NS hdfs-ci-krb5-0 -- rm /tmp/hdfs.keytab || :
rm hdfs.keytab || :
kubectl exec -n $HADOOP_NS hdfs-ci-krb5-0 -- kadmin.local -q "ktadd -norandkey -k /tmp/hdfs.keytab $ALL_PRINCIPALS"
kubectl cp -n $HADOOP_NS hdfs-ci-krb5-0:/tmp/hdfs.keytab hdfs.keytab
KEYTAB_SECRET=hdfs-ci-krb5-keytab
kubectl delete secret -n $HADOOP_NS $KEYTAB_SECRET || :
kubectl create secret generic -n $HADOOP_NS $KEYTAB_SECRET --from-file=hdfs.keytab

# SPILLY setup a single secret that has the krb5.conf and krb5.keytab.  That way we are in sync with the operator
