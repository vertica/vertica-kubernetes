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

# A script that will generate the YAML manifests we use for release artifacts.

set -o errexit
set -o pipefail

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )"
REPO_DIR=$(dirname $SCRIPT_DIR)
MANIFEST_DIR=$1

if [ -z $MANIFEST_DIR ]
then
    echo "*** Must specify directory to find the manifests"
    exit 1
fi

if [ ! -d $MANIFEST_DIR ]
then
    echo "*** The directory $MANIFEST_DIR doesn't exist"
    exit 1
fi

CRD_DIR=$2
if [ -z $CRD_DIR ]
then
    echo "*** Must specify directory to find the crds"
    exit 1
fi

if [ ! -d $CRD_DIR ]
then
    echo "*** The directory $CRD_DIR doesn't exist"
    exit 1
fi

# Copy out manifests that we will include as release artifacts.  We do this
# *before* templating so that they can used directly with a 'kubectl apply'
# command.
RELEASE_ARTIFACT_TARGET_DIR=$REPO_DIR/config/release-manifests
mkdir -p $RELEASE_ARTIFACT_TARGET_DIR
for f in verticadb-operator-metrics-monitor-servicemonitor.yaml \
    verticadb-operator-proxy-rolebinding-crb.yaml \
    verticadb-operator-proxy-role-cr.yaml \
    verticadb-operator-metrics-reader-cr.yaml \
    verticadb-operator-metrics-reader-crb.yaml
do
  cp $MANIFEST_DIR/$f $RELEASE_ARTIFACT_TARGET_DIR
  # Modify the artifact we are copying over by removing any namespace field.
  # We cannot infer the namespace.  In most cases, the namespace can be
  # supplied when applying the manifests.  For the ClusterRoleBinding it will
  # produce an error.  But this is better then substituting in some random
  # namespace that might not exist on the users system.
  perl -i -0777 -pe 's/.*namespace:.*\n//g' $RELEASE_ARTIFACT_TARGET_DIR/$f
done

# Copy the Role that allows users to work with the CRs that the verticadb
# operator manages.
cp $REPO_DIR/config/rbac/verticadb-operator-cr-user-role.yaml $RELEASE_ARTIFACT_TARGET_DIR
# Copy the Role that must be linked to the ServiceAccount running the vertica
# server pods.
cp $REPO_DIR/config/rbac/vertica-server-role.yaml $RELEASE_ARTIFACT_TARGET_DIR

# Create a single YAML with all of the CRDs.
megaCRD=$RELEASE_ARTIFACT_TARGET_DIR/crds.yaml
rm $megaCRD || true
for f in $CRD_DIR/*-crd.yaml
do
    cat $f >> $megaCRD
    echo "---" >> $megaCRD
done
sed -i '$ d' $megaCRD  # Remove the last '---'
