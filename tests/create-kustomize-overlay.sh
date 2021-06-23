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

set -o errexit

DEF_IMAGE_NAME="verticadocker/vertica-k8s:latest"
LICENSE=

function usage {
    echo "usage: $0 [-vh] [-l <licenseName>] [<imageName>]"
    echo
    echo "  <imageName>    Image name to use for VerticaDB CRD."
    echo "                 If omitted, it defaults to $DEF_IMAGE_NAME "
    echo
    echo "Options:"
    echo "  -v                 Verbose output"
    echo "  -l <licenseName>   Include the given license in each VerticaDB file"
    echo
    exit 1
}

OPTIND=1
while getopts "hvl:" opt; do
    case ${opt} in
        h)
            usage
            ;;
        v)
            set -o xtrace
            VERBOSE=1
            ;;
        l)
            LICENSE=$OPTARG
            ;;
        \?)
            echo "Unknown option: -${opt}"
            usage
            ;;
    esac
done

IMAGE_NAME=${@:$OPTIND:1}
if [ -z "${IMAGE_NAME}" ]; then
    IMAGE_NAME=$DEF_IMAGE_NAME
fi
echo "Using image name: $IMAGE_NAME"
if [ -n "$LICENSE" ]; then
    echo "Using license name: $LICENSE"
fi

function create_kustomization {
    BASE_DIR=$1
    echo "" > kustomization.yaml
    kustomize edit add base $BASE_DIR
    kustomize edit set image replace-with-kustomize=$IMAGE_NAME

    # If license was specified we create a patch file to set that.
    if [[ -n "$LICENSE" ]]
    then
        LICENSE_PATCH_FILE="license-patch.yaml"
        cat <<EOF > $LICENSE_PATCH_FILE
        - op: add
          path: /spec/licenseSecret
          value: $LICENSE
EOF
        kustomize edit add patch --path $LICENSE_PATCH_FILE --kind VerticaDB --version v1beta1 --group vertica.com
    fi
}

function create_pod_kustomization {
    TC_OVERLAY=$1/overlay
    mkdir -p $TC_OVERLAY
    pushd $TC_OVERLAY > /dev/null
    if [[ -n "$VERBOSE" ]]; then
        echo "Creating overlay in $TC_OVERLAY"
    fi
    create_kustomization ../base
    popd > /dev/null
}

# Descend into each test and create the overlay kustomization.
# The overlay is created in a directory like: overlay/<tc-name>
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )"
cd $SCRIPT_DIR
for tdir in e2e/*/*/base e2e-disabled/*/*/base
do
    create_pod_kustomization $(dirname $tdir)
done
for tdir in manifests/*
do
    create_pod_kustomization $tdir
done
