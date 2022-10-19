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
  sed -i 's/.*namespace:.*//g' $RELEASE_ARTIFACT_TARGET_DIR/$f
done

# Generate a single manifest that all of the rbac rules to run the operator.
# This is a release artifact too, so it must be free of any templating.
OPERATOR_RBAC=$RELEASE_ARTIFACT_TARGET_DIR/operator-rbac.yaml
rm $OPERATOR_RBAC 2>/dev/null || :
touch $OPERATOR_RBAC
chmod +x $OPERATOR_RBAC
for f in verticadb-operator-controller-manager-sa.yaml \
    verticadb-operator-leader-election-role-role.yaml \
    verticadb-operator-manager-role-role.yaml \
    verticadb-operator-leader-election-rolebinding-rb.yaml \
    verticadb-operator-manager-rolebinding-rb.yaml \
    verticadb-operator-manager-clusterrolebinding-crb.yaml \
    verticadb-operator-manager-role-cr.yaml
do
    cat $MANIFEST_DIR/$f >> $OPERATOR_RBAC
    echo "---" >> $OPERATOR_RBAC
done
perl -i -0777 -pe 's/.*namespace:.*\n//g' $OPERATOR_RBAC
sed -i '$ d' $OPERATOR_RBAC   # Remove the last line of the file
