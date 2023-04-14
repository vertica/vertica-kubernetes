#!/bin/bash

# (c) Copyright [2021-2022] Open Text.
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
KILL_CONTAINER=1

function usage {
    echo "usage: $0 [-kv]"
    echo
    echo "Options:"
    echo "  -k  Keep the vertica-k8s container running on exit"
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
        k)
            unset KILL_CONTAINER
            ;;
        \?)
            echo "Unknown option: -${opt}"
            usage
            ;;
    esac
done

# Pull the image locally if not already present
if [[ "$(docker images -q $VERTICA_IMG 2> /dev/null)" == "" ]]
then
    docker pull $VERTICA_IMG
fi

# Make sure the vertica image isn't a minimal container.  We can only extract
# the sdk from a full image because we remove it for the minimal one.
MINIMAL_IMG=$(docker inspect $VERTICA_IMG -f {{.Config.Labels.minimal}})
if [ "$MINIMAL_IMG" == "YES" ]
then
    echo "We cannot setup e2e udx with the image '$VERTICA_IMG' because it was created as minimal and doesn't have the Vertica SDK"
    exit 1
fi

# We need to get the udx examples that we use for the e2e test.  These are in
# the vertica-k8s image.
VERTICA_CONTAINER=$(docker run -it --rm -d $VERTICA_IMG)
function cond_kill_container() {
    [[ -n $KILL_CONTAINER ]] && docker kill $VERTICA_CONTAINER
}
trap cond_kill_container EXIT

# Wait for the container to be running
until [ "$(docker inspect -f {{.State.Running}} $VERTICA_CONTAINER)" == "true" ]
do 
  sleep 0.1 
done

rm -rf $REPO_DIR/sdk 2>/dev/null
docker cp $VERTICA_CONTAINER:/opt/vertica/sdk $REPO_DIR/sdk
mkdir -p $REPO_DIR/bin
docker cp $VERTICA_CONTAINER:/opt/vertica/bin/VerticaSDK.jar $REPO_DIR/bin/VerticaSDK.jar

export SDK_HOME=$REPO_DIR/sdk
export SDK_JAR=$REPO_DIR
cd $SDK_HOME/examples
export CXX=c++  # Ensure we go through linux alternatives to find the compiler
export JAVA_BUILDINFO=$REPO_DIR/sdk/BuildInfo.java
# Hack to get around non-standard location of the sdk.  The makefile doesn't
# allow us to override the location of the BuildInfo.java.  This sed command
# will let us control that from the command line.
sed -i 's/JAVA_BUILDINFO :=/JAVA_BUILDINFO ?=/g' makefile
make

# Show the contents of the files we copied over and built
find $REPO_DIR/bin
find $REPO_DIR/sdk
