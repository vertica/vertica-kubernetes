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

DEF_TAG=kind

# The make targets and  the invoked shell scripts are directly run from the root directory.
function usage {
    echo "$0 -l <log_dir>  -n <cluster_name> -t <tag_name> [-h]"
    echo "  l   Log directory.   default: PWD"
    echo "  n   Name of the kind cluster. default: vertica"
    echo "  t   Tag. default: $DEF_TAG"
    exit
}

OPTIND=1
while getopts l:n:t:h opt; do
    case ${opt} in
        l)
            INT_TEST_OUTPUT_DIR=${OPTARG}
            ;;
        n)
            CLUSTER_NAME=${OPTARG}
            ;;
        t)
            TAG=${OPTARG}
            ;;
        h)
            usage
            ;;
        \?)
            echo "ERROR: unrecognized option"
            usage
            ;;
    esac
done
shift "$((OPTIND-1))"

#Sanity Checks

if [ -z ${CLUSTER_NAME} ]; then
    CLUSTER_NAME=vertica
    echo "Assigned default value 'vertica' to CLUSTER_NAME"
fi

if [ -z ${TAG} ]; then
    TAG=$DEF_TAG
    echo "Assigned default value '$TAG' to TAG"
fi

if [ -z ${INT_TEST_OUTPUT_DIR} ]; then
    INT_TEST_OUTPUT_DIR=${PWD}
fi


PACKAGES_DIR=docker-vertica/packages #RPM file should be in this directory to create docker image.
RPM_FILE=vertica-x86_64.RHEL6.latest.rpm
export INT_TEST_OUTPUT_DIR
export VERTICA_IMG=vertica-k8s:$TAG
export OPERATOR_IMG=verticadb-operator:$TAG
export WEBHOOK_IMG=verticadb-webhook:$TAG

# cleanup the deployed k8s cluster
function cleanup {
    make clean-deploy clean-int-tests 2> /dev/null # Removes the installed vertica chart and integration tests
    scripts/kind.sh term $CLUSTER_NAME
}

# Copy rpm to the PACKAGES_DIR for the image to be built
function copy_rpm {
    #This expects the rpm in $INT_TEST_OUTPUT_DIR and copies the file to $PACKAGES_DIR
    cp -p "$INT_TEST_OUTPUT_DIR"/"$RPM_FILE" "$PACKAGES_DIR"/"$RPM_FILE"
}

# Setup the k8s cluster and switch context
function setup_cluster {
    echo "Setting up kind cluster named $CLUSTER_NAME"
    scripts/kind.sh  init "$CLUSTER_NAME"
    kubectx kind-"$CLUSTER_NAME"
}

# Build vertica images and push them to the kind environment
function build_and_push {
    echo "Building all of the container images"
    make  docker-build vdb-gen
    echo "Pushing the images to the kind cluster"
    make  docker-push
}

# Run integration tests and store the pod status in a file
function run_integration_tests {
  echo "Saving the test status log in $INT_TEST_OUTPUT_DIR/integration_run.log "
  make run-int-tests > "$INT_TEST_OUTPUT_DIR"/integration_run.log
}

trap cleanup EXIT
copy_rpm
setup_cluster
build_and_push
run_integration_tests
cleanup
