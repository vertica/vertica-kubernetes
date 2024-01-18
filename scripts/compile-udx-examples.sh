#!/usr/bin/env bash

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

# Run this in the container to compile the sdk examples. It depends on the
# vertica-kubernetes repo to mounted in the volume as /repo
# docker run -i --mount type=bind,src=<repo>,dst=/repo <vertica-k8s-image> /repo/scripts/compile-udx-examples.sh

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )"
REPO_DIR=$(dirname $SCRIPT_DIR)

source $SCRIPT_DIR/logging-utils.sh

set -o errexit
set -o pipefail

if [ -z $TARGET_UID ]
then
    echo "TARGET_UID environment variable needs to be set"
    exit 1
fi
if [ -z $TARGET_GID ]
then
    echo "TARGET_GID environment variable needs to be set"
    exit 1
fi

logInfo Install the packages needed
if which apt-get # ubuntu / apt-get
then
    apt-get update && sudo apt-get install -y g++ libboost-all-dev libcurl4-openssl-dev libbz2-dev bzip2 perl openjdk-8-jdk
else # yum package manager
    yum install -y gcc-c++ boost-devel libcurl-devel bzip2-devel bzip2 perl java-1.8.0-openjdk-devel zlib-devel
fi

logInfo Setup dev environment
function set_file_permissions() {
    chown --recursive $TARGET_UID $REPO_DIR/sdk || true
    chgrp --recursive $TARGET_GID $REPO_DIR/sdk || true
    chown --recursive $TARGET_UID $REPO_DIR/bin || true
    chgrp --recursive $TARGET_GID $REPO_DIR/bin || true
}
trap set_file_permissions EXIT
export SDK_HOME=$REPO_DIR/sdk
export SDK_JAR=$REPO_DIR
mkdir -p $REPO_DIR/bin
cp /opt/vertica/bin/VerticaSDK.jar $REPO_DIR/bin/VerticaSDK.jar
cd $SDK_HOME/examples
export CXX=c++  # Ensure we go through linux alternatives to find the compiler
export JAVA_BUILDINFO=$REPO_DIR/sdk/BuildInfo.java
# Hack to get around non-standard location of the sdk.  The makefile doesn't
# allow us to override the location of the BuildInfo.java.  This perl command
# will let us control that from the command line.
perl -i -0777 -pe 's/JAVA_BUILDINFO :=/JAVA_BUILDINFO ?=/g' makefile
logInfo Build the examples
make

logInfo Show result of build
# Show the contents of the files we built
find $REPO_DIR/bin
find $REPO_DIR/sdk
