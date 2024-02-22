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

# A script that will setup for the e2e udx tests by compiling the example programs

set -o errexit
set -o pipefail

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )"
REPO_DIR=$(dirname $SCRIPT_DIR)

source $SCRIPT_DIR/logging-utils.sh

function usage {
    echo "usage: $0 [-v] <samples-image> <build-image>"
    echo
    echo "There are two images that are required to be passed in:"
    echo "<samples-image> Is the name of the image to pull the samples from."
    echo "                This typically is \$VERTICA_IMG."
    echo "<build-image> The name of the image to use to build the samples."
    echo "              This differs as the samples cannot be built with"
    echo "              the latest GCC compiler, so tends to be an older image."
    echo
    echo "Options:"
    echo "  -v  Show verbose output"
    echo
    exit 1
}

OPTIND=1
while getopts "hkv" opt; do
    case ${opt} in
        h)
            usage
            ;;
        v)
            set -o xtrace
            ;;
        \?)
            echo "Unknown option: -${opt}"
            usage
            ;;
    esac
done

if [ $(( $# - $OPTIND )) -lt 1 ]
then
    echo "*** Must provide the name of two images to use to build the samples"
    usage
fi
SAMPLES_IMG=${@:$OPTIND:1}
BUILDER_IMG=${@:$OPTIND+1:2}
logInfo "Samples image is $SAMPLES_IMG"
logInfo "Builder image is $BUILDER_IMG"

# Pull both image locally if not already present
if [[ "$(docker images -q $SAMPLES_IMG 2> /dev/null)" == "" ]]
then
    docker pull $SAMPLES_IMG
fi
if [[ "$(docker images -q $BUILDER_IMG 2> /dev/null)" == "" ]]
then
    docker pull $BUILDER_IMG
fi

# Make sure the samples image isn't a minimal container.  We can only extract
# the sdk from a full image because we remove it for the minimal one.
MINIMAL_IMG=$(docker inspect $SAMPLES_IMG -f {{.Config.Labels.minimal}})
if [ "$MINIMAL_IMG" == "YES" ]
then
    echo "We cannot setup e2e udx with the image $SAMPLES_IMG because it was created as minimal and doesn't have the Vertica SDK"
    exit 1
fi

logInfo Pull the samples from the samples image: $SAMPLES_IMG
rm -rf $REPO_DIR/sdk || true
SAMPLES_CONTAINER=$(docker create $SAMPLES_IMG --entrypoint bash)
docker cp $SAMPLES_CONTAINER:/opt/vertica/sdk $REPO_DIR
mkdir -p $REPO_DIR/bin
rm -f $REPO_DIR/bin/VerticaSDK.jar || true
docker cp $SAMPLES_CONTAINER:/opt/vertica/bin/VerticaSDK.jar $REPO_DIR/bin
docker rm $SAMPLES_CONTAINER

logInfo Compile the samples with image: $BUILDER_IMG
docker \
    run \
    -i \
    --rm \
    --mount type=bind,src=$REPO_DIR,dst=/repo \
    --user root \
    --env TARGET_UID=$(id -u) \
    --env TARGET_GID=$(id -g) \
    --entrypoint /repo/scripts/compile-udx-examples.sh \
    $BUILDER_IMG
