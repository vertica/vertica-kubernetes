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

# This generates the commands to run to authenticate pods with Kerberos.  It
# generates the commands to generate the service principals, then a command to
# construct a keytab that has to be mounted in the Vertica container.

set -o errexit
set -o pipefail

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )"
REPO_DIR=$(dirname $SCRIPT_DIR)
REALM=VERTICACORP.COM
SERVICE_NAME=vertica

function usage {
    echo "usage: $0 [-r <realm>] [-s <serviceName>] <namespace> <vdbName> <subclusterName> <subclusterSize> [<subclusterName> <subclusterSize> ...]"
    echo
    echo "Options:"
    echo "  -r     Name of the realm.  Defaults to '${REALM}'."
    echo "  -s     Service name of the principal. Defaults to '${SERVICE_NAME}'."
    echo
    echo "Positional Arguments:"
    echo " <namespace>       Name of the namespace the VerticaDB is deployed in"
    echo " <vdbName>         Name of the VerticaDB CR"
    echo " <subclusterName>  Name of the subcluster"
    echo " <subclusterSize>  Size fo the subcluster"
    exit 1
}

OPTIND=1
while getopts "hr:s:" opt
do
    case $opt in
        r) REALM=$OPTARG;;
        s) SERVICE_NAME=$OPTARG;;
        h) usage;;
    esac
done
shift "$((OPTIND-1))"

if [ $(( $# - $OPTIND )) -lt 3 ]
then
    echo "*** Missing positional arguments"
    usage
fi

NAMESPACE=$1; shift 1
VDB_NAME=$1; shift 1

ALL_PRINCIPALS=
while [ "$#" -gt 0 ]
do
    SUBCLUSTER_NAME=$1; shift 1 
    SUBCLUSTER_SIZE=$1; shift 1 

    for POD_INDEX in $(seq 0 $(( $SUBCLUSTER_SIZE - 1 )))
    do
        PRINCIPAL="$SERVICE_NAME/${VDB_NAME}-${SUBCLUSTER_NAME}-${POD_INDEX}.${VDB_NAME}.${NAMESPACE}.svc.cluster.local@${REALM}"
        ALL_PRINCIPALS+="$PRINCIPAL "
        echo "kadmin.local -q \"ank -randkey $PRINCIPAL\""
    done
done

if [ -n "$ALL_PRINCIPALS" ]
then
    echo "kadmin.local -q \"ktadd -norandkey -k krb5.keytab $ALL_PRINCIPALS\""
fi