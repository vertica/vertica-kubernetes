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

# Script that will compare the image against a version. It returns the result
# in the form of an error code.

set -e

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )"
REPO_DIR=$(dirname $SCRIPT_DIR)
IMG=$VERTICA_IMG

source $SCRIPT_DIR/logging-utils.sh
source $SCRIPT_DIR/image-utils.sh

function usage() {
    echo "usage: $0 -i <image> <older> <version>"
    echo
    echo "Options:"
    echo "  -i     Name of the image to compare the version against."
    echo "         Defaults to $VERTICA_IMG"
    echo
    echo "Positional Arguments:"
    echo " <older>   Key word for the comparison"
    echo " <version> What version to compare against"
    exit 1
}

while getopts "hi:" opt
do
    case $opt in
        h) usage;;
        i) IMG=$OPTARG;;
    esac
done

if [ $(( $# - $OPTIND )) -lt 1 ]
then
    usage
fi
COMPARE_TYPE=${@:$OPTIND:1}
COMPARE_VERSION=${@:$OPTIND+1:1}

if [[ "$COMPARE_TYPE" -ne "older" ]]
then
    echo "*** $COMPARES_TYPE is an invalid comparison. Must be: older"
    usage
fi

logAndRunCommand docker pull $IMG
# Normally we would rely on the vertica-version label of the image but it is empty
# for the latest-test-master image. Until we figure out why, we are adding
# this work around to not skip some tests.
if [[ "$IMG" == "docker.io/opentext/vertica-k8s-private:latest-test-master" ]]
then
    logInfo "$IMG is newer"
    exit 1
fi
IMG_VER=$(determine_image_version $IMG)
logInfo "Image $IMG has version $IMG_VER"
logInfo "Checking if $IMG_VER is $COMPARE_TYPE than $COMPARE_VERSION"
IFS='.' read img_major img_minor img_patch <<< "$IMG_VER"
IFS='.' read cmp_major cmp_minor cmp_patch <<< "$COMPARE_VERSION"

if [[ "$img_major" -lt "$cmp_major" ]]
then
    logInfo "$IMG is older"
    exit 0
elif [[ "$img_major" -gt "$cmp_major" ]]
then
    logInfo "$IMG is newer"
    exit 1
fi

if [[ "$img_minor" -lt "$cmp_minor" ]]
then
    logInfo "$IMG is older"
    exit 0
elif [[ "$img_minor" -gt "$cmp_minor" ]]
then
    logInfo "$IMG is newer"
    exit 1
fi

if [[ "$img_patch" -lt "$cmp_patch" ]]
then
    logInfo "$IMG is older"
    exit 0
elif [[ "$img_patch" -gt "$cmp_patch" ]]
then
    logInfo "$IMG is newer"
    exit 1
else
    logInfo "$IMG is the same version"
    exit 1
fi
