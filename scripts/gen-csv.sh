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

# A script that will generate the ClusterServiceVersion.

set -o errexit
set -o pipefail

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )"
REPO_DIR=$(dirname $SCRIPT_DIR)
OPERATOR_SDK=$REPO_DIR/bin/operator-sdk
KUSTOMIZE=$REPO_DIR/bin/kustomize
USE_IMAGE_DIGESTS_FLAG=

function usage {
    echo "usage: $0 [options] <version> <bundle_metadata_opts>"
    echo
    echo "<version> is the version of the operator."
    echo
    echo "<bundle_metadata_opts> are extra options to pass to"
    echo "'operator-sdk generate bundle'"
    echo
    echo "Options:"
    echo "  -u    To enable the use of SHA Digest for image"
    echo "  -h    help flag"
    echo
    exit 1
}

OPTIND=1
while getopts "uh" opt; do
    case ${opt} in
        u)
            USE_IMAGE_DIGESTS_FLAG="--use-image-digests"
            ;;
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
BUNDLE_GEN_FLAGS="-q --overwrite --version $VERSION $BUNDLE_METADATA_OPTS $USE_IMAGE_DIGESTS_FLAG"

set -o xtrace

cd $REPO_DIR
rm -rf bundle/ 2>/dev/null || true
$OPERATOR_SDK generate kustomize manifests -q
mkdir -p config/overlays/csv
cd config/overlays/csv
cat <<- EOF > kustomization.yaml
bases:
- ../../manifests
EOF
$KUSTOMIZE edit set image controller=$OPERATOR_IMG
cd $REPO_DIR
$KUSTOMIZE build config/overlays/csv | $OPERATOR_SDK generate bundle $BUNDLE_GEN_FLAGS

# Fill in the placeholders
sed -i "s/CREATED_AT_PLACEHOLDER/$(date +"%FT%H:%M:%SZ")/g" bundle/manifests/verticadb-operator.clusterserviceversion.yaml
sed -i "s+OPERATOR_IMG_PLACEHOLDER+$(make echo-images | grep OPERATOR_IMG | cut -d'=' -f2)+g" bundle/manifests/verticadb-operator.clusterserviceversion.yaml

# Delete the ServiceMonitor object from the bundle.  This puts a
# requirement on having the Prometheus Operator installed.  We are only
# optionally installing this.  We will include the manifest in our GitHub
# artifacts and have it as an optional helm parameter.
rm bundle/manifests/*servicemonitor.yaml

# Add the supported versions at the end of annotations.yaml
cat <<EOT >> bundle/metadata/annotations.yaml

  # Annotation to specify the supported versions.
  # This annotation has been added because for now, the operator does not work
  # with versions lower than v4.8.
  com.redhat.openshift.versions: "v4.8"
EOT
