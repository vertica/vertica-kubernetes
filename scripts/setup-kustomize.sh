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
    VERTICA_IMG=$(cd $REPO_DIR && make echo-images | grep ^VERTICA_IMG= | cut -d'=' -f2)
fi
if [ -z "${BASE_VERTICA_IMG}" ]; then
    BASE_VERTICA_IMG=$(cd $REPO_DIR && make echo-images | grep ^BASE_VERTICA_IMG= | cut -d'=' -f2)
fi
if [ -z "${VLOGGER_IMG}" ]; then
    VLOGGER_IMG=$(cd $REPO_DIR && make echo-images | grep VLOGGER_IMG | cut -d'=' -f2)
fi

# Name of the secret that contains the cert to use for communal access
# authentication.  This is the name of the namespace copy, so it is hard coded
# in this script.
COMMUNAL_EP_CERT_SECRET_NS_COPY="communal-ep-cert"
# Similar hard coded name for namespace specific hadoopConfig
HADOOP_CONF_CM_NS_COPY="hadoop-conf"
# The full prefix for the communal path
if [ "$PATH_PROTOCOL" == "azb://" ]
then
  if [ -n "$BLOB_ENDPOINT_HOST" ]
  then
    COMMUNAL_PATH_PREFIX=${PATH_PROTOCOL}${BUCKET_OR_CLUSTER}@${BLOB_ENDPOINT_HOST}/${CONTAINER_NAME}${PATH_PREFIX}
  else
    COMMUNAL_PATH_PREFIX=${PATH_PROTOCOL}${BUCKET_OR_CLUSTER}/${CONTAINER_NAME}${PATH_PREFIX}
  fi
else
  COMMUNAL_PATH_PREFIX=${PATH_PROTOCOL}${BUCKET_OR_CLUSTER}${PATH_PREFIX}
fi

echo "Vertica server image name: $VERTICA_IMG"
echo "Vertica logger image name: $VLOGGER_IMG"
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

    if [ "$PATH_PROTOCOL" == "s3://" ] || [ "$PATH_PROTOCOL" == "gs://" ]
    then
      cat <<EOF >> kustomization.yaml
    - op: replace
      path: /spec/communal/endpoint
      value: $ENDPOINT
    - op: replace
      path: /spec/communal/credentialSecret
      value: communal-creds
    - op: replace
      path: /spec/communal/region
      value: $REGION
EOF
    elif [ "$PATH_PROTOCOL" == "azb://" ]
    then
      cat <<EOF >> kustomization.yaml
    - op: replace
      path: /spec/communal/credentialSecret
      value: communal-creds
EOF
    elif [ "$PATH_PROTOCOL" == "webhdfs://" ] || [ "$PATH_PROTOCOL" == "swebhdfs://" ]
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
        - name: v-base-upgrade
  - source:
      kind: ConfigMap
      name: e2e
      fieldPath: data.baseVerticaImage
    targets:
      - select:
          kind: VerticaDB
          name: v-base-upgrade
        fieldPaths:
          - spec.image
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
      fieldPath: data.verticaImage
    targets:
      - select:
          kind: Job
        fieldPaths:
          - spec.template.spec.containers.0.image
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
    elif [ "$PATH_PROTOCOL" == "gs://" ]
    then
      cat <<EOF >> kustomization.yaml
    - op: replace
      path: /spec/containers/0/command/2
      value: "printf \"$ACCESSKEY\n$SECRETKEY\n\" | gsutil config -a && (gsutil -m rm -r ${PATH_PROTOCOL}${BUCKET_OR_CLUSTER}${PATH_PREFIX}${TESTCASE_NAME} || true)"
    - op: replace
      path: /spec/containers/0/image
      value: google/cloud-sdk:360.0.0-alpine
EOF
    # Azure when not using blob endpoint.  This assumes we are using the real
    # Azure service in the cloud.  'az storage delete-batch' is much too slow.
    # 'az storage remove' is quicker.
    elif [ "$PATH_PROTOCOL" == "azb://" ] && [ -z "$BLOB_ENDPOINT_HOST" ]
    then
      cat <<EOF >> kustomization.yaml
    - op: replace
      path: /spec/containers/0/command/2
      value: az storage remove --account-name $BUCKET_OR_CLUSTER --container-name $CONTAINER_NAME --name ${PATH_PREFIX:1}${TESTCASE_NAME} --recursive
    - op: replace
      path: /spec/containers/0/image
      value: mcr.microsoft.com/azure-cli:2.29.0
EOF
      if [ -n "$ACCOUNT_KEY" ]
      then
        cat <<EOF >> kustomization.yaml
    - op: add
      path: /spec/containers/0/env/-
      value:
        name: AZURE_STORAGE_KEY
        value: $ACCOUNT_KEY
EOF
      elif [ -n "$SHARED_ACCESS_SIGNATURE" ]
      then
        cat <<EOF >> kustomization.yaml
    - op: add
      path: /spec/containers/0/env/-
      value:
        name: AZURE_STORAGE_SAS_TOKEN
        value: "$SHARED_ACCESS_SIGNATURE"
EOF
      else
        echo "1 *** No credentials setup for azb://"
        exit 1
      fi
    # Azure when using a blob endpoint.  This assumes we are using Azurite.
    # 'az storage remove' doesn't work (see https://github.com/Azure/azure-cli/issues/19311),
    # so we rely on 'az storage blob delete-batch'.
    elif [ "$PATH_PROTOCOL" == "azb://" ] && [ -n "$BLOB_ENDPOINT_HOST" ]
    then
      if [ -z "$ACCOUNT_KEY" ]
      then
        echo "*** When using blob endpoint, expecting an account key"
        exit 1
      fi

      cat <<EOF >> kustomization.yaml
    - op: replace
      path: /spec/containers/0/command/2
      value: az storage blob delete-batch --account-name $BUCKET_OR_CLUSTER --source $CONTAINER_NAME --pattern "${PATH_PREFIX:1}${TESTCASE_NAME}/*"
    - op: replace
      path: /spec/containers/0/image
      value: mcr.microsoft.com/azure-cli:2.29.0
    - op: add
      path: /spec/containers/0/env/-
      value:
        name: AZURE_STORAGE_CONNECTION_STRING
        value: "DefaultEndpointsProtocol=$BLOB_ENDPOINT_PROTOCOL;AccountName=$BUCKET_OR_CLUSTER;AccountKey=$ACCOUNT_KEY;BlobEndpoint=$BLOB_ENDPOINT_PROTOCOL://$BLOB_ENDPOINT_HOST/$BUCKET_OR_CLUSTER;"
EOF
    elif [ "$PATH_PROTOCOL" == "webhdfs://" ] || [ "$PATH_PROTOCOL" == "swebhdfs://" ]
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
  verticaImage: ${VERTICA_IMG}
  vloggerImage: ${VLOGGER_IMG}
  baseVerticaImage: ${BASE_VERTICA_IMG}
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

    if [ "$PATH_PROTOCOL" == "s3://" ] || [ "$PATH_PROTOCOL" == "gs://" ] || [ "$PATH_PROTOCOL" == "azb://" ]
    then
      if [ "$PATH_PROTOCOL" != "azb://" ]
      then
        cat <<EOF > creds.yaml
apiVersion: v1
kind: Secret
metadata:
  name: communal-creds
type: Opaque
data:
  accesskey: $(echo -n "$ACCESSKEY" | base64)
  secretkey: $(echo -n "$SECRETKEY" | base64)
EOF
      else
        cat <<EOF > creds.yaml
apiVersion: v1
kind: Secret
metadata:
  name: communal-creds
type: Opaque
data:
  accountName: $(echo -n "$BUCKET_OR_CLUSTER" | base64)
EOF
        if [ -n "$BLOB_ENDPOINT_HOST" ]
        then
          cat <<EOF >> creds.yaml
  blobEndpoint: $(echo -n "$BLOB_ENDPOINT_PROTOCOL://$BLOB_ENDPOINT_HOST" | base64 --wrap 0)
EOF

          # When using Azurite you can only connect using a single word hostname
          # or IP.  We include a service object so that it is created in the
          # same namespace that we are deploying VerticaDB.  It will create a
          # single word hostname (azure) that maps to the service in the
          # kuttl-e2e-azb namespace.  For example we can access Azurite with
          # this: http://azurite:10000
          AZURITE_NS=kuttl-e2e-azb
          if kubectl get -n $AZURITE_NS svc azurite > /dev/null
          then
            AZURITE_SVC=ext-svc.yaml
            cat <<EOF > $AZURITE_SVC
kind: Service
apiVersion: v1
metadata:
  name: azurite
spec:
  type: ExternalName
  externalName: azurite.$AZURITE_NS.svc.cluster.local
EOF
          fi
        fi

        if [ -n "$ACCOUNT_KEY" ]
        then
          cat <<EOF >> creds.yaml
  accountKey: $(echo -n "$ACCOUNT_KEY" | base64 --wrap 0)
EOF
        elif [ -n "$SHARED_ACCESS_SIGNATURE" ]
        then
          cat <<EOF >> creds.yaml
  sharedAccessSignature: $(echo -n "$SHARED_ACCESS_SIGNATURE" | base64 --wrap 0)
EOF
        else
          echo "*** No credentials setup for azb://"
          exit 1
        fi
      fi

      cat <<EOF > kustomization.yaml
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
- ../base
- creds.yaml
EOF
      # Include the ExternalName service object if one was created.
      if [ -n "$AZURITE_SVC" ]
      then
        cat <<EOF >> kustomization.yaml
- $AZURITE_SVC
EOF
      fi
    elif [ "$PATH_PROTOCOL" == "webhdfs://" ] || [ "$PATH_PROTOCOL" == "swebhdfs://" ]
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
    elif [ "$PATH_PROTOCOL" == "webhdfs://" ] || [ "$PATH_PROTOCOL" == "swebhdfs://" ] || \
         [ "$PATH_PROTOCOL" == "gs://" ] || [ "$PATH_PROTOCOL" == "azb://" ]
    then
      cat <<EOF > kustomization.yaml
# Intentionally blank -- either no permissions to create a bucket or one doesn't exist for protocol.
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
for tdir in e2e/*/*/base e2e-extra/*/*/base e2e-online-upgrade/*/*/base e2e-operator-upgrade-overlays/*/*/base
do
    create_vdb_pod_kustomization $(dirname $tdir) $(basename $(realpath $tdir/../..))
done
for tdir in e2e/* e2e-extra/* e2e-disabled/* e2e-online-upgrade/* e2e-operator-upgrade-overlays/*
do
    clean_communal_kustomization $tdir
done
