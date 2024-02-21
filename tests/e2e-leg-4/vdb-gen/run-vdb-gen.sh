#!/bin/bash

# (c) Copyright [2021-2024] Open Text.
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

# Script to be run inside the container that calls vdb-gen

set -o errexit
set -o xtrace
set -o pipefail

NAMESPACE=$1
VERTICA_IMG=$2
VDB_NAME=$3
COMMUNAL_EP_CERT_SECRET=$4
DEPLOYMENT_METHOD=$5
VDB_USER=$6

# The ca.cert is optional.
if [ -f "/certs/$COMMUNAL_EP_CERT_SECRET/ca.crt" ]
then
    CA_CERT_OPT="-cafile /certs/$COMMUNAL_EP_CERT_SECRET/ca.crt"
fi

/tmp/vdb-gen \
    -license /home/dbadmin/licensing/ce/vertica_community_edition.license.key \
    -image $VERTICA_IMG \
    -user $VDB_USER \
    -name $VDB_NAME \
    -password superuser \
    -ignore-cluster-lease \
    $CA_CERT_OPT \
    -depot-volume EmptyDir \
    -deployment-method $DEPLOYMENT_METHOD \
    v-vdb-gen-sc2-0.v-vdb-gen.$NAMESPACE vertdb
