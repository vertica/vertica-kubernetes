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
source $SCRIPT_DIR/image-utils.sh

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

TARGET_IMAGE=${@:$OPTIND:1}
PUBLIC_IMAGE=vertica-k8s

# We use to have a strict upgrade path policy. So, it was important to pick the
# preceding image version. However, we have since relaxed that. The only factor
# in picking the base image is the deployment type. vclusterops we cannot pick
# a version prior to 24.1.0 since that was the first version we supported that
# deployment method. For admintools deployments, we can pick any version
# supported by the operator so arbitrarily I picked 12.0.2.
if [[ $VERTICA_DEPLOYMENT_METHOD == vclusterops ]]
then
    print_vertica_k8s_img $PUBLIC_IMAGE 24 1 0
else
    print_vertica_k8s_img $PUBLIC_IMAGE 12 0 2
fi
