#!/bin/bash

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

# A script that will return an image name to use as the base image to upgrade
# from. The caller gives the target image. We will parse the image, guessing
# what the vertica version is. Then produce an image that follows a proper
# upgrade path.

set -o errexit
set -o pipefail

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )"
REPO_DIR=$(dirname $SCRIPT_DIR)

source $SCRIPT_DIR/logging-utils.sh

function usage() {
    echo "usage: $0 <image-name>"
    echo
    echo "Positional Arguments:"
    echo " <image-name>   Name of the image that we will upgrade too."
    exit 1
}

while getopts "h" opt
do
    case $opt in
      h) usage;;
    esac
done

if [ $(( $# - $OPTIND )) -lt 0 ]
then
    logError "Image name is required"
    usage
fi

function printVerticaK8sImg
{
    major=$1
    minor=$2
    patch=$3
    echo "vertica/vertica-k8s:$major.$minor.$patch-0"
}

function decideVersionAndExitIfFound
{
    major=$1
    minor=$2

    # 23.3.x is a special case because that was the first verson after 12.0.4
    if [[ "$major" == "23" && "$minor" == "3" ]]
    then
        printVerticaK8sImg 12 0 2
        exit 0
    # Guess the image based on the versioning pattern of '<year>.<quarter>.0'
    elif [[ "$major" -ge "23" ]]
    then
        if [[ "$minor" > 1 ]]
        then
            printVerticaK8sImg $major $(($minor - 1)) 0
        else
            printVerticaK8sImg $(($major - 1)) 4 0
        fi
        exit 0
    # Legacy case from before we switched to '<year>.<quarter>.0' versioning
    elif [[ "$major" == "12" ]]
    then
        printVerticaK8sImg 12 0 2
        exit 0
    fi
}

function getRPMVersion
{
    # Find the RPM version that's download and built for the CI
    grep 'VERTICA_CE_URL:' $REPO_DIR/.github/actions/download-rpm/action.yaml | cut -d':' -f3 | cut -d'/' -f5 | cut -d'-' -f2
}

TARGET_IMAGE=${@:$OPTIND:1}
LAST_RELEASED_IMAGE=$(printVerticaK8sImg 23 3 0)

# Extract out the tag from the image.
IFS=':' read image tag <<< "$TARGET_IMAGE"

if [[ -z "$tag" ]]
then
   # No tag found. Assume latest, so pick the last released image
   echo $LAST_RELEASED_IMAGE
   exit 0
fi

IFS='.' read major minor patch <<< "$tag"

# If we were able to extract only digits for major/minor, then the tag was
# in fact a version.
if [[ $major =~ ^[0-9]+$ && $minor =~ ^[0-9]+$ ]]
then
    decideVersionAndExitIfFound $major $minor
fi

# No able to figure out the version from the tag.  If the image repo is
# dockerhub, then we assume we are running with the nightly build, which is the
# latest master. So, return the last released image.
if [[ $TARGET_IMAGE == docker.io/* ]]
then
    echo $LAST_RELEASED_IMAGE
    exit 0
# We assume we are running with an image built in this CI that used the public
# RPM. This is true for PRs or running off of main
else
    IFS='.' read major minor patch <<< "$(getRPMVersion)"
    decideVersionAndExitIfFound $major $minor
fi

echo "Unable to guess the server upgrade base image"
exit 1
