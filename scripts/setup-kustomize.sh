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

# Populate the kustomize and its overlay to run e2e tests.

set -o errexit
set -o pipefail

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )"
REPO_DIR=$(dirname $SCRIPT_DIR)
KUSTOMIZE=$REPO_DIR/bin/kustomize
HDFS_NS=kuttl-e2e-hdfs

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

# Name of the secret that contains the cert to use for communal access
# authentication.  This is the name of the namespace copy, so it is hard coded
# in this script.
COMMUNAL_EP_CERT_SECRET_NS_COPY="communal-ep-cert"
# Similar hard coded name for namespace specific hadoopConfig
HADOOP_CONF_CM_NS_COPY="hadoop-conf"
# The full prefix for the communal path
COMMUNAL_PATH_PREFIX=${PATH_PROTOCOL}${BUCKET_OR_CLUSTER}${PATH_PREFIX}

echo "Vertica server image name: $VERTICA_IMG"
echo "Vertica logger image name: $VLOGGER_IMG"
if [ -n "$LICENSE_SECRET" ]; then
    echo "License name: $LICENSE_SECRET"
fi
echo "Endpoint: $ENDPOINT"
echo "Protocol: $PATH_PROTOCOL"
echo "S3 bucket name or cluster name: $BUCKET_OR_CLUSTER"
echo "Communal Path Prefix: $PATH_PREFIX"

function create_vdb_kustomization {
    BASE_DIR=$1
    TESTCASE_NAME=$2

    cat <<EOF > kustomization.yaml
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
  - $BASE_DIR
  - $(realpath --relative-to="." $REPO_DIR/tests/kustomize-base)

patches:
- target:
    version: v1beta1
    kind: VerticaDB
  patch: |-
    - op: replace
      path: /spec/communal/path
      value: ${COMMUNAL_PATH_PREFIX}${TESTCASE_NAME}
EOF

    if [ "$PATH_PROTOCOL" == "s3://" ]
    then
      cat <<EOF >> kustomization.yaml
    - op: replace
      path: /spec/communal/endpoint
      value: $ENDPOINT
    - op: replace
      path: /spec/communal/credentialSecret
      value: s3-creds
    - op: replace
      path: /spec/communal/region
      value: $REGION
EOF
    elif [ "$PATH_PROTOCOL" == "webhdfs://" ]
    then
        if [ -n "$HADOOP_CONF_CM" ]
        then
            cat <<EOF >> kustomization.yaml
    - op: replace
      path: /spec/communal/hadoopConfig
      value: $HADOOP_CONF_CM_NS_COPY
EOF
        fi
    else
      echo "*** Unknown protocol: $PATH_PROTOCOL"
      exit 1
    fi

      cat <<EOF >> kustomization.yaml
replacements:
  - source:
      kind: ConfigMap
      name: e2e
      fieldPath: data.verticaImage
    targets:
      - select:
          kind: VerticaDB
        fieldPaths:
          - spec.image
        reject:
        - name: v-upgrade-vertica
  - source:
      kind: ConfigMap
      name: e2e
      fieldPath: data.verticaImage
    targets:
      - select:
          kind: Pod
        fieldPaths:
          - spec.containers.0.image
  - source:
      kind: ConfigMap
      name: e2e
      fieldPath: data.vloggerImage
    targets:
      - select:
          kind: VerticaDB
        fieldPaths:
          - spec.sidecars.[name=vlogger].image
EOF

    if [ -n "$COMMUNAL_EP_CERT_SECRET" ]
    then
        cat <<EOF >> kustomization.yaml
  - source:
      kind: ConfigMap
      name: e2e
      fieldPath: data.caFile
    targets:
      - select:
          kind: VerticaDB
        fieldPaths:
          - spec.communal.caFile
        options:
          create: true
EOF

        COMMUNAL_EP_CERT_SECRET_PATCH="communal-ep-cert-secret-patch.yaml"
        cat <<EOF > $COMMUNAL_EP_CERT_SECRET_PATCH
        - op: add
          path: /spec/certSecrets/-
          value: 
            name: $COMMUNAL_EP_CERT_SECRET_NS_COPY
EOF
        $KUSTOMIZE edit add patch --path $COMMUNAL_EP_CERT_SECRET_PATCH --kind VerticaDB
    fi

    # If license was specified we create a patch file to set that.
    if [[ -n "$LICENSE_SECRET" ]]
    then
        LICENSE_PATCH_FILE="license-patch.yaml"
        cat <<EOF > $LICENSE_PATCH_FILE
        - op: add
          path: /spec/licenseSecret
          value: $LICENSE_SECRET
EOF
        $KUSTOMIZE edit add patch --path $LICENSE_PATCH_FILE --kind VerticaDB --version v1beta1 --group vertica.com
    fi

}

function create_vdb_pod_kustomization {
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
    create_vdb_kustomization ../base $2
    popd > /dev/null
}

function clean_communal_kustomization {
    if [ ! -d $1 ]
    then
      return 0
    fi

    TC_OVERLAY=$1/clean-communal/overlay
    TESTCASE_NAME=$(basename $1)
    mkdir -p $TC_OVERLAY
    pushd $TC_OVERLAY > /dev/null
    cat <<EOF > kustomization.yaml
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
- ../../../../manifests/clean-communal/base
patches:
- target:
    version: v1
    kind: Pod
    name: clean-communal
  patch: |-
EOF

    if [ "$PATH_PROTOCOL" == "s3://" ]
    then
      cat <<EOF >> kustomization.yaml
    - op: replace
      path: /spec/containers/0/command/2
      value: "aws s3 rm --recursive --endpoint $ENDPOINT s3://${BUCKET_OR_CLUSTER}${PATH_PREFIX}${TESTCASE_NAME} --no-verify-ssl"
    - op: replace
      path: /spec/containers/0/image
      value: amazon/aws-cli:2.2.24
    - op: add
      path: /spec/containers/0/env/-
      value:
        name: AWS_ACCESS_KEY_ID
        value: $ACCESSKEY
    - op: add
      path: /spec/containers/0/env/-
      value:
        name: AWS_SECRET_ACCESS_KEY
        value: $SECRETKEY
    - op: add
      path: /spec/containers/0/env/-
      value:
        name: AWS_EC2_METADATA_DISABLED
        value: 'true'
EOF
    elif [ "$PATH_PROTOCOL" == "webhdfs://" ]
    then
      cat <<EOF >> kustomization.yaml
    - op: replace
      path: /spec/containers/0/command/2
      value: "hadoop fs -rm -r -f -skipTrash ${PATH_PREFIX}${TESTCASE_NAME}"
    - op: replace
      path: /spec/containers/0/image
      value: uhopper/hadoop:2.7.2
    - op: add
      path: /spec/containers/0/env
      value:
        - name: HADOOP_CONF_DIR
          value: /etc/hadoop/conf
        - name: HADOOP_USER_NAME
          value: hdfs
    - op: add
      path: /spec/containers/0/volumeMounts
      value:
        - name: hdfs-config
          mountPath: /etc/hadoop/conf
          readOnly: true
    - op: add
      path: /spec/volumes
      value:
        - name: hdfs-config
          configMap:
            name: hadoop-conf
EOF
    elif [ "$PATH_PROTOCOL" == "gs://" ]
    then
      true  # Nothing for now SPILLY
    else
      echo "*** Unknown protocol: $PATH_PROTOCOL"
      exit 1
    fi
    popd > /dev/null
}

function create_communal_cfg {
    pushd kustomize-base > /dev/null
    cat <<EOF > e2e.yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: e2e
data:
  verticaImage: ${VERTICA_IMG:-$DEF_VERTICA_IMG}
  vloggerImage: ${VLOGGER_IMG:-$DEF_VLOGGER_IMG}
EOF

    # If a cert was specified for communal endpoint access, include a datapoint
    # for the container relative location of the cert.
    if [ -n "$COMMUNAL_EP_CERT_SECRET" ]
    then
        cat <<EOF >> e2e.yaml
  caFile: /certs/$COMMUNAL_EP_CERT_SECRET_NS_COPY/ca.crt
  caFileSecretName: $COMMUNAL_EP_CERT_SECRET_NS_COPY
EOF
    fi

    popd > /dev/null
}

function copy_communal_ep_cert {
    pushd kustomize-base > /dev/null
    if [ -z "$COMMUNAL_EP_CERT_SECRET" ]
    then
        # Communal endpoint cert isn't set.  We will just create an empty file
        # so that kustomize doesn't complain about a missing resource.
        echo "" > communal-ep-cert.json
    else
        # Copy the secret over stripping out all of the metadata.
        kubectl get secrets -o json -n $COMMUNAL_EP_CERT_NAMESPACE $COMMUNAL_EP_CERT_SECRET \
        | jq 'del(.metadata)' \
        | jq ".metadata += {name: \"$COMMUNAL_EP_CERT_SECRET_NS_COPY\"}" > communal-ep-cert.json
    fi
    popd > /dev/null
}

function copy_hadoop_conf {
    pushd kustomize-base > /dev/null
    if [ -z "$HADOOP_CONF_CM" ]
    then
        # No hadoop conf configMap is present.  We will just create an empty
        # file so that kustomize doesn't complain about a missing resource.
        echo "" > hadoop-conf.json
    else
        # Copy the secret over stripping out all of the metadata.
        kubectl get configmap -o json -n $HADOOP_CONF_NAMESPACE $HADOOP_CONF_CM \
        | jq 'del(.metadata)' \
        | jq ".metadata += {name: \"$HADOOP_CONF_CM_NS_COPY\"}" > hadoop-conf.json
    fi
    popd > /dev/null
}

function create_communal_creds {
    pushd manifests/communal-creds > /dev/null

    mkdir -p overlay
    pushd overlay > /dev/null

    if [ "$PATH_PROTOCOL" == "s3://" ]
    then
      cat <<EOF > creds.yaml
apiVersion: v1
kind: Secret
metadata:
  name: s3-creds
type: Opaque
data:
  accesskey: $(echo -n "$ACCESSKEY" | base64)
  secretkey: $(echo -n "$SECRETKEY" | base64)
EOF

      cat <<EOF > kustomization.yaml
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
- ../base
- creds.yaml
EOF
    elif [ "$PATH_PROTOCOL" == "webhdfs://" ]
    then
      cat <<EOF > kustomization.yaml
resources:
- ../base
EOF
    else
      echo "*** Unknown protocol: $PATH_PROTOCOL"
      exit 1
    fi
    
    popd > /dev/null
    popd > /dev/null
}

function setup_creds_for_create_s3_bucket {
    pushd manifests/create-s3-bucket > /dev/null

    mkdir -p overlay
    pushd overlay > /dev/null

    if [ "$PATH_PROTOCOL" == "s3://" ]
    then
      cat <<EOF > kustomization.yaml
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
- ../base

patches:
- target:
    version: v1
    kind: Pod
    name: create-s3-bucket
  patch: |-
    - op: replace
      path: /spec/containers/0/env/0/value
      value: $BUCKET_OR_CLUSTER
    - op: replace
      path: /spec/containers/0/env/1/value
      value: $ACCESSKEY
    - op: replace
      path: /spec/containers/0/env/2/value
      value: $SECRETKEY
    - op: replace
      path: /spec/containers/0/env/4/value
      value: $ENDPOINT
EOF
    elif [ "$PATH_PROTOCOL" == "webhdfs://" ]
    then
      cat <<EOF > kustomization.yaml
# Intentionally blank -- no s3 bucket to create for HDFS.
EOF
    else
      echo "*** Unknown protocol: $PATH_PROTOCOL"
      exit 1
    fi
    
    popd > /dev/null
    popd > /dev/null
}

cd $REPO_DIR/tests

# Create the configMap that is used to control the communal endpoint and creds.
create_communal_cfg
# Copy over the cert that was used to set up the communal endpoint
copy_communal_ep_cert
# Copy over the hadoop conf configMap.  This may be set for HDFS communal paths.
copy_hadoop_conf
# Setup the communal credentials according to the protocol used
create_communal_creds
# Setup an overlay for create-s3-bucket so it has access to the credentials
setup_creds_for_create_s3_bucket

# Descend into each test and create the overlay kustomization.
# The overlay is created in a directory like: overlay/<tc-name>
for tdir in e2e/*/*/base
do
    create_vdb_pod_kustomization $(dirname $tdir) $(basename $(realpath $tdir/../..))
done
for tdir in e2e/* e2e-disabled/*
do
    clean_communal_kustomization $tdir
done
