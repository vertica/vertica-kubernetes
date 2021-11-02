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

# A script that will generate the ClusterServiceVersion.

set -o errexit
set -o pipefail

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )"
REPO_DIR=$(dirname $SCRIPT_DIR)
OPERATOR_SDK=$REPO_DIR/bin/operator-sdk
KUSTOMIZE=$REPO_DIR/bin/kustomize

function usage {
    echo "usage: $0 <version> <bundle_metadata_opts>"
    echo
    echo "<version> is the version of the operator."
    echo
    echo "<bundle_metadata_opts> are extra options to pass to"
    echo "'operator-sdk generate bundle'"
    echo
    exit 1
}

OPTIND=1
while getopts "h" opt; do
    case ${opt} in
        h)
            usage
            ;;
        \?)
            echo "Unknown option: -${opt}"
            usage
            ;;
    esac
done

if [ $(( $# - $OPTIND )) -lt 2 ]
then
    usage
fi

VERSION=${@:$OPTIND:1}
BUNDLE_METADATA_OPTS=${@:$OPTIND+1:1}

set -o xtrace

cd $REPO_DIR
$OPERATOR_SDK generate kustomize manifests -q
mkdir -p config/overlays/csv
cd config/overlays/csv
cat <<- EOF > kustomization.yaml
bases:
- ../../manifests
EOF
$KUSTOMIZE edit set image controller=$OPERATOR_IMG
cd $REPO_DIR
$KUSTOMIZE build config/overlays/csv | $OPERATOR_SDK generate bundle -q --overwrite --version $VERSION $BUNDLE_METADATA_OPTS

# Fill in the placeholders
sed -i "s/CREATED_AT_PLACEHOLDER/$(date +"%FT%H:%M:%SZ")/g" bundle/manifests/verticadb-operator.clusterserviceversion.yaml
sed -i "s+OPERATOR_IMG_PLACEHOLDER+$(make echo-images | grep OPERATOR_IMG | cut -d'=' -f2)+g" bundle/manifests/verticadb-operator.clusterserviceversion.yaml
