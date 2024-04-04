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

# Image utilities to be sourced into various bash scripts

PUBLIC_REPO=vertica
PRIVATE_REPO=opentext
PRIVATE_IMAGE=vertica-k8s-private
PUBLIC_IMAGE=vertica-k8s

function print_vertica_k8s_img
{
    repo=$1
    imageName=$2
    major=$3
    minor=$4
    patch=$5
    print_vertica_k8s_img_with_tag $repo $imageName "$major.$minor.$patch-0"
}

function print_vertica_k8s_img_with_tag
{
    repo=$1
    imageName=$2
    tag=$3
    echo "${repo}/$imageName:$tag"
}

function get_rpm_version 
{
    local SCRIPT_DIR REPO_DIR
    SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )"
    REPO_DIR=$(dirname $SCRIPT_DIR)
    # Find the RPM version that's download and built for the CI
    grep 'VERTICA_CE_URL:' $REPO_DIR/.github/actions/download-rpm/action.yaml | cut -d':' -f3 | cut -d'/' -f5 | cut -d'-' -f2
}

function get_vertica_image_version 
{
    imageName=$1
    local SCRIPT_DIR REPO_DIR
    SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )"
    REPO_DIR=$(dirname $SCRIPT_DIR)
    docker inspect $imageName -f '{{ $ver := index .Config.Labels "vertica-version"}}{{ $ver }}' | cut -d'-' -f1
}

function determine_image_version() {
    local TARGET_IMAGE=$1

    # If the image is something that actually exists already, we can simply
    # read from the labels what vertica version was used.
    if docker inspect $TARGET_IMAGE > /dev/null 2>&1 || \
        docker pull $TARGET_IMAGE > /dev/null 2>&1
    then
        IFS='.' read major minor patch <<< "$(get_vertica_image_version $TARGET_IMAGE)"
        echo "$major.$minor.$patch"
        return
    fi

    # Extract out the tag from the image.
    IFS=':' read image tag <<< "$TARGET_IMAGE"
    IFS='.' read major minor patch <<< "$tag"

    # If we were able to extract only digits for major/minor, then the tag was
    # in fact a version.
    if [[ $major =~ ^[0-9]+$ && $minor =~ ^[0-9]+$ ]]
    then
        echo "$major.$minor.0"
        return
    fi

    # We assume we are running with an image built in this CI that used the public
    # RPM. This is true for PRs or running off of main
    IFS='.' read major minor patch <<< "$(get_rpm_version)"
    echo "$major.$minor.$patch"
    return
}
