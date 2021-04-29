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

TAG=latest

while getopts "t:" opt
do
    case $opt in
        t) TAG=$OPTARG;;
    esac
done

if [ $(( $# - $OPTIND )) -lt 0 ]
then
    echo "usage: push-to-kind.sh [-t <tag>] <name>"
    echo
    echo "Options:"
    echo "  -t     Tag to use for the images.  Defaults to latest"
    echo
    echo "Positional Arguments:"
    echo " <name>  Name of the cluster to push to"
    exit 1
fi
CLUSTER_NAME=${@:$OPTIND:1}

if [[ -z $GOPATH ]]
then
    GOPATH=$HOME
fi
PATH=$GOPATH/go/bin:$PATH

for imageName in vertica-k8s python-tools
do
    kind load docker-image --name ${CLUSTER_NAME} ${imageName}:${TAG}
done
