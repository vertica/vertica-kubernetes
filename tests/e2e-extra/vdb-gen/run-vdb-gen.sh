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

# Script to be run inside the container that calls vdb-gen

set -o errexit
set -o xtrace
set -o pipefail

NAMESPACE=$1
VERTICA_IMG=$2
COMMUNAL_EP_CERT_SECRET=$3

# The ca.cert is optional.
if [ -f "/certs/$COMMUNAL_EP_CERT_SECRET/ca.crt" ]
then
    CA_CERT_OPT="-cafile /certs/$COMMUNAL_EP_CERT_SECRET/ca.crt"
fi

HADOOP_CONF=/etc/hadoop
if [ -d "$HADOOP_CONF" ]
then
    HADOOP_CONF_OPT="-hadoopConfig $HADOOP_CONF"
    # We need to be strict about the name of the CA Cert because when using
    # swebhdfs:// config files in /etc/hadoop hard code the path to
    # /certs/communal-ep-cert.
    CA_CERT_NAME_OPT="-cacertname communal-ep-cert"
fi

/tmp/vdb-gen \
    -license /home/dbadmin/licensing/ce/vertica_community_edition.license.key \
    -image $VERTICA_IMG \
    -name v-vdb-gen-revive \
    -password superuser \
    -ignore-cluster-lease \
    $CA_CERT_OPT \
    $CA_CERT_NAME_OPT \
    $HADOOP_CONF_OPT \
    v-vdb-gen-sc2-0.v-vdb-gen.$NAMESPACE vertdb
