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

# Push images to kind -- kubernetes in docker

set -e

function setImageWithTag() {
    TAG=$1
    IMAGES="vertica-k8s:$TAG verticadb-operator:$TAG verticadb-webhook:$TAG vertica-logger:$TAG"
}

TAG=kind
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )"
REPO_DIR=$(dirname $SCRIPT_DIR)
KIND=$REPO_DIR/bin/kind
setImageWithTag $TAG

function usage() {
    echo "usage: push-to-kind.sh [-i <image>] [-t <tag>] [-f <file>] [<name>]"
    echo
    echo "Options:"
    echo "  -i     Image names to push.  Defaults to: $IMAGES"
    echo "  -t     Tag name to use with each image.  This is mutually exclusive"
    echo "         with -i as it will set the image names with this tag."
    echo "         Defaults to: $TAG"
    echo "  -f     Read a list of images from the given file.  If file is '-',"
    echo "         it will read from stdin."
    echo
    echo "Positional Arguments:"
    echo " <name>  Name of the cluster to push to.  If unset, it will pick from the current context."
    exit 1
}

while getopts "i:t:f:h" opt
do
    case $opt in
        t)
            setImageWithTag $OPTARG
            ;;
        i)
            IMAGES=$OPTARG
            ;;
        h) 
            usage
            ;;
        f)
            if [ "$OPTARG" = "-" ]
            then
              OPTARG=/dev/stdin
            fi
            # Build an image list from the file and strip out any comments
            IMAGES=$(cat $OPTARG | sed '/^[[:blank:]]*#/d;s/#.*//')
            ;;
        \?)
            echo "ERROR: unrecognized option: -$opt"
            usage
            ;;
    esac
done

GREEN='\033[0;32m'
ORANGE='\033[0;33m'
YELLOW='\033[1;33m'
NC='\033[0m'  # No color

if [ $(( $# - $OPTIND )) -lt 0 ]
then
    # Typical kind cluster name is something like: kind-matt1
    # We only want what is after 'kind-'
    # Need to use "2-" after -f option to indidate everything after and including the 2nd field (in case we have cluster name like kind-cluster-test)
    CLUSTER_NAME=$(kubectl config current-context | cut -d"-" -f2-)
else
    CLUSTER_NAME=${@:$OPTIND:1}
fi
printf "${YELLOW}Pushing to cluster ${GREEN}${CLUSTER_NAME}${NC}\n"

if [[ -z $GOPATH ]]
then
    GOPATH=$HOME
fi
PATH=$GOPATH/go/bin:$PATH

for imageName in $IMAGES
do
    printf "${YELLOW}Image: ${GREEN}$imageName${NC}\n"
    # Image must exist locally before pushing to kind
    if [[ "$(docker images -q $imageName 2> /dev/null)" == "" ]]
    then
      printf "${ORANGE}Image not present locally, doing a docker pull${NC}\n"
      # Retry to avoid any temporary network issue
      for i in $(seq 1 5); do docker pull $imageName && break || sleep 60; done
    fi

    ${KIND} load docker-image --name ${CLUSTER_NAME} ${imageName}
done
