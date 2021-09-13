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

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )"
REPO_DIR=$(dirname $SCRIPT_DIR)

function usage {
    echo "usage: $0 [-hv] [<configFile>]"
    echo
    echo "Use config files to control the endpoints and credentials used.  This script always"
    echo "overrides the defaults with the config file set in the KUSTOMIZE_CFG environment"
    echo "variable.  You can also specify a config file as an option, this will override"
    echo "anything in KUSTOMIZE_CFG."
    echo
    echo "You can use kustomize-defaults.cfg as a base for the config file."
    echo
    echo "Options:"
    echo "  -v                 Verbose output"
    echo
    exit 1
}

OPTIND=1
while getopts "hv" opt; do
    case ${opt} in
        h)
            usage
            ;;
        v)
            set -o xtrace
            VERBOSE=1
            ;;
        \?)
            echo "Unknown option: -${opt}"
            usage
            ;;
    esac
done

USER_CONFIG_FILE=${@:$OPTIND:1}

# Read in the defaults
source $REPO_DIR/tests/kustomize-defaults.cfg

# Override any of the defaults if the KUSTOMIZE_CFG points to a file.
if [ -n "$KUSTOMIZE_CFG" ]
then
  source $KUSTOMIZE_CFG
fi

# Override any of the defaults from the users config file if provided.
if [ -n "$USER_CONFIG_FILE" ]
then
  source $USER_CONFIG_FILE
fi

if [ -z "${VERTICA_IMG}" ]; then
    VERTICA_IMG=$DEF_VERTICA_IMG
fi
if [ -z "${VLOGGER_IMG}" ]; then
    VLOGGER_IMG=$DEF_VLOGGER_IMG
fi

echo "Using vertica server image name: $VERTICA_IMG"
echo "Using vertica logger image name: $VLOGGER_IMG"
if [ -n "$LICENSE_SECRET" ]; then
    echo "Using license name: $LICENSE_SECRET"
fi
echo "Using endpoints: $ENDPOINT_1, $ENDPOINT_2"

function create_kustomization {
    BASE_DIR=$1
    echo "" > kustomization.yaml
    kustomize edit add base $BASE_DIR
    kustomize edit set image kustomize-vertica-image=$VERTICA_IMG
    kustomize edit set image kustomize-vlogger-image=$VLOGGER_IMG

    # If license was specified we create a patch file to set that.
    if [[ -n "$LICENSE_SECRET" ]]
    then
        LICENSE_PATCH_FILE="license-patch.yaml"
        cat <<EOF > $LICENSE_PATCH_FILE
        - op: add
          path: /spec/licenseSecret
          value: $LICENSE_SECRET
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
  endpoint1: $ENDPOINT_1
  accesskeyEnc1: $(echo -n $ACCESSKEY_1 | base64)
  secretkeyEnc1: $(echo -n $SECRETKEY_1 | base64)
  accesskeyUnenc1: $ACCESSKEY_1
  secretkeyUnenc1: $SECRETKEY_1

  endpoint2: $ENDPOINT_2
  accesskeyEnc2: $(echo -n $ACCESSKEY_2 | base64)
  secretkeyEnc2: $(echo -n $SECRETKEY_2 | base64)
  accesskeyUnenc2: $ACCESSKEY_2
  secretkeyUnenc2: $SECRETKEY_2
EOF

    popd > /dev/null
}

cd $SCRIPT_DIR

# Create the configMap that is used to control the communal endpoint and creds.
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
