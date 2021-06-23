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

# Push images to kind -- kubernetes in docker

set -e

function setImageWithTag() {
    TAG=$1
    IMAGES="vertica-k8s:$TAG verticadb-operator:$TAG verticadb-webhook:$TAG"
}

TAG=kind
setImageWithTag $TAG

function usage() {
    echo "usage: push-to-kind.sh [-i <image>] [-t <tag>] [<name>]"
    echo
    echo "Options:"
    echo "  -i     Image names to push.  Defaults to: $IMAGES"
    echo "  -t     Tag name to use with each image.  This is mutually exclusive"
    echo "         with -i as it will set the image names with this tag."
    echo "         Defaults to: $TAG"
    echo
    echo "Positional Arguments:"
    echo " <name>  Name of the cluster to push to.  If unset, it will pick from the current context."
    exit 1
}

while getopts "i:t:h" opt
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
        \?)
            echo "ERROR: unrecognized option: -$opt"
            usage
            ;;
    esac
done

if [ $(( $# - $OPTIND )) -lt 0 ]
then
    # Typical kind cluster name is something like: kind-matt1
    # We only want what is after 'kind-'
    CLUSTER_NAME=$(kubectl config current-context | cut -d"-" -f2)
else
    CLUSTER_NAME=${@:$OPTIND:1}
fi
echo "Pushing to cluster ${CLUSTER_NAME}"

if [[ -z $GOPATH ]]
then
    GOPATH=$HOME
fi
PATH=$GOPATH/go/bin:$PATH

for imageName in $IMAGES
do
    kind load docker-image --name ${CLUSTER_NAME} ${imageName}
done
