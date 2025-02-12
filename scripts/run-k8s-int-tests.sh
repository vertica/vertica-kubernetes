#!/bin/bash

# (c) Copyright [2021-2024] Open Text.
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
set -o pipefail

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )"
REPO_DIR=$(dirname $SCRIPT_DIR)
BUILD_IMAGES=1
INT_TEST_OUTPUT_DIR=${REPO_DIR}/int-tests-output
CLUSTER_NAME=vertica
EXTRA_EXTERNAL_IMAGE_FILE=
MOUNT_PATH_ARG=

# The make targets and  the invoked shell scripts are directly run from the root directory.
function usage {
    echo "$0 -l <log_dir>  -n <cluster_name> -e <ext-image-file> [-hs]"
    echo
    echo "Options:"
    echo "  -l <log_dir>        Log directory.   default: $INT_TEST_OUTPUT_DIR"
    echo "  -n <cluster_name>   Name of the kind cluster. default: $CLUSTER_NAME"
    echo "  -e <ext-image-file> File with list of additional images to pull prior to running e2e tests"
    echo "  -s                  Skip the building of the container images we publish externally"
    echo "  -m <path>           Host path volume to mount in kind"
    exit
}

OPTIND=1
while getopts l:n:hse:m: opt; do
    case ${opt} in
        l)
            INT_TEST_OUTPUT_DIR=${OPTARG}
            ;;
        n)
            CLUSTER_NAME=${OPTARG}
            ;;
        s)
            BUILD_IMAGES=
            ;;
        e)
            EXTRA_EXTERNAL_IMAGE_FILE=${OPTARG}
            if [ ! -f "$EXTRA_EXTERNAL_IMAGE_FILE" ]
            then
                echo "*** File '$EXTRA_EXTERNAL_IMAGE_FILE' does not exist"
                exit 1
            fi
            ;;
        h)
            usage
            ;;
        m)
            MOUNT_PATH_ARG="-m ${OPTARG}"
            ;;
        \?)
            echo "ERROR: unrecognized option"
            usage
            ;;
    esac
done
shift "$((OPTIND-1))"

#Sanity Checks

PACKAGES_DIR=docker-vertica/packages #RPM file should be in this directory to create docker image.
RPM_FILE=vertica-x86_64.RHEL6.latest.rpm
RPM_PATH="${PACKAGES_DIR}/${RPM_FILE}"
# It is expected that the caller has set the appropriate *_IMG environment variables
export INT_TEST_OUTPUT_DIR
export PATH=$PATH:$HOME/.krew/bin

# cleanup the deployed k8s cluster
function cleanup {
    # Collect scrutinize for any failed tests.
    scripts/capture-scrutinize.sh -n kuttl-test || :
    df -h
    scripts/kind.sh term $CLUSTER_NAME
}

function setup_env {
    mkdir -p $INT_TEST_OUTPUT_DIR
}

# Setup the k8s cluster and switch context
function setup_cluster {
    echo "Setting up kind cluster named $CLUSTER_NAME"
    scripts/kind.sh $MOUNT_PATH_ARG init "$CLUSTER_NAME"
    kubectx kind-"$CLUSTER_NAME"
}

# Build vertica images and push them to the kind environment
function build {
    if [ ! -f "$RPM_PATH" ]
    then
        echo "*** RPM not found in expected path: $RPM_PATH"
        exit 1
    fi

    echo "Building all of the container images"
    make docker-build

    df -h
}

function build_push_olm_bundle {
    deploy_with=$(make echo-vars | grep 'DEPLOY_WITH=' | cut -d'=' -f2)
    echo "Deployment method: $deploy_with"
    if [ "$deploy_with" = "olm" ]
    then
        # This one is handle separately because its not a container that we publish
        # externally.  So it needs to be build each time we run the e2e tests.
        echo "Building and pushing the OLM bundle"
        make docker-build-bundle docker-push-bundle
    fi
}

# Build vertica images and push them to the kind environment
function push {
    echo "Pushing the images to the kind cluster"
    make  docker-push
    echo "Pushing the external images to the kind cluster"
    scripts/push-to-kind.sh -f tests/external-images-common-ci.txt
    if [ -n "$EXTRA_EXTERNAL_IMAGE_FILE" ]
    then
        scripts/push-to-kind.sh -f $EXTRA_EXTERNAL_IMAGE_FILE
    fi
}

# Run integration tests and store the pod status in a file
function run_integration_tests {
  echo "Saving the test status log in $INT_TEST_OUTPUT_DIR/integration_run.log "
  make run-int-tests | tee "$INT_TEST_OUTPUT_DIR"/kuttl.out
}

trap cleanup EXIT
setup_env
setup_cluster
if [ -n "$BUILD_IMAGES" ]
then
    build
fi
push
build_push_olm_bundle
run_integration_tests
cleanup
