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

DEF_VERTICA_IMAGE_NAME="vertica/vertica-k8s:latest"
DEF_VLOGGER_IMAGE_NAME="vertica/vertica-logger:latest"
LICENSE=
ENDPOINTS=http://minio.kuttl-e2e-communal,http://minio.kuttl-e2e-communal

function usage {
    echo "usage: $0 [-vh] [-l <licenseName>] [-e <endpoints>] [<imageName> [<vloggerImageName>]] "
    echo
    echo "  <imageName>         Image name to use in the VerticaDB CR."
    echo "                      If omitted, it defaults to $DEF_VERTICA_IMAGE_NAME "
    echo "  <vloggerImageName>  Image name to use for the vertica logger sidecar in"
    echo "                      the VerticaDB CR.  If omitted, it defaults to $DEF_VLOGGER_IMAGE_NAME "
    echo
    echo "Options:"
    echo "  -v                 Verbose output"
    echo "  -l <licenseName>   Include the given license in each VerticaDB file"
    echo "  -e <endpoints>     List of communal endpoints to use.  It is a comma separated list.  "
    echo "                     Order matters, so first endpoint will be used for any testcase that"
    echo "                     wants to use data.endpoint1"
    echo
    exit 1
}

OPTIND=1
while getopts "hvl:e:" opt; do
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
        e)
            ENDPOINTS=$OPTARG
            ;;
        \?)
            echo "Unknown option: -${opt}"
            usage
            ;;
    esac
done

VERTICA_IMAGE_NAME=${@:$OPTIND:1}
if [ -z "${VERTICA_IMAGE_NAME}" ]; then
    VERTICA_IMAGE_NAME=$DEF_VERTICA_IMAGE_NAME
    VLOGGER_IMAGE_NAME=$DEF_VLOGGER_IMAGE_NAME
else
    VLOGGER_IMAGE_NAME=${@:$OPTIND+1:2}
fi
if [ -z "${VLOGGER_IMAGE_NAME}" ]; then
    VLOGGER_IMAGE_NAME=$DEF_VLOGGER_IMAGE_NAME
fi

echo "Using vertica server image name: $VERTICA_IMAGE_NAME"
echo "Using vertica logger image name: $VLOGGER_IMAGE_NAME"
if [ -n "$LICENSE" ]; then
    echo "Using license name: $LICENSE"
fi
echo "Using endpoints: $ENDPOINTS"

function create_kustomization {
    BASE_DIR=$1
    echo "" > kustomization.yaml
    kustomize edit add base $BASE_DIR
    kustomize edit set image kustomize-vertica-image=$VERTICA_IMAGE_NAME
    kustomize edit set image kustomize-vlogger-image=$VLOGGER_IMAGE_NAME

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
    # Skip directory if it doesn't have any kustomization config
    if [ ! -d $1/base ]
    then
      return 0
    fi

    TC_OVERLAY=$1/overlay
    mkdir -p $TC_OVERLAY
    pushd $TC_OVERLAY > /dev/null
    if [[ -n "$VERBOSE" ]]; then
        echo "Creating overlay in $TC_OVERLAY"
    fi
    create_kustomization ../base
    popd > /dev/null
}

function create_s3_bucket_kustomization {
    if [ ! -d $1 ]
    then
      return 0
    fi

    EP=$2
    TC_OVERLAY=$1/create-s3-bucket-$EP/overlay
    mkdir -p $TC_OVERLAY
    pushd $TC_OVERLAY > /dev/null
    cat <<EOF > kustomization.yaml
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
- ../../../../manifests/create-s3-bucket-$EP/base
patches:
- target:
    version: v1
    kind: Pod
    name: create-s3-bucket
  patch: |-
    - op: replace
      path: "/spec/containers/0/env/0"
      value:
        name: S3_BUCKET
        value: $(basename $1)
EOF
    popd > /dev/null
}

function clean_s3_bucket_kustomization {
    if [ ! -d $1 ]
    then
      return 0
    fi

    EP=$2
    TC_OVERLAY=$1/clean-s3-bucket-$EP/overlay
    mkdir -p $TC_OVERLAY
    pushd $TC_OVERLAY > /dev/null
    cat <<EOF > kustomization.yaml
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
- ../../../../manifests/clean-s3-bucket-$EP/base
patches:
- target:
    version: v1
    kind: Pod
    name: clean-s3-bucket
  patch: |-
    - op: replace
      path: "/spec/containers/0/env/0"
      value:
        name: S3_BUCKET
        value: $(basename $1)
EOF
    popd > /dev/null
}

function create_communal_cfg {
    pushd kustomize-base > /dev/null
    cat <<EOF > communal-cfg.yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: e2e
data:
EOF
    IFS=',' read -ra EPS <<< "$ENDPOINTS"
    count=1
    for i in "${EPS[@]}"
    do
        echo "  endpoint${count}: $i" >> communal-cfg.yaml
        (( count++ ))
    done

    popd > /dev/null
}

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )"
cd $SCRIPT_DIR

# Create the configMap that is used to control the communal endpoint for each test.
create_communal_cfg

# Descend into each test and create the overlay kustomization.
# The overlay is created in a directory like: overlay/<tc-name>
for tdir in e2e/*/*/base e2e-disabled/*/*/base
do
    create_pod_kustomization $(dirname $tdir)
done
for tdir in manifests/*
do
    create_pod_kustomization $tdir
done
for tdir in e2e/* e2e-disabled/*
do
    create_s3_bucket_kustomization $tdir ep1
    create_s3_bucket_kustomization $tdir ep2
    clean_s3_bucket_kustomization $tdir ep1
    clean_s3_bucket_kustomization $tdir ep2
done
