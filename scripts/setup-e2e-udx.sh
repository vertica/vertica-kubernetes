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

# A script that will setup for the e2e udx tests by compiling the example programs

set -o errexit
set -o pipefail

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )"
REPO_DIR=$(dirname $SCRIPT_DIR)

function usage {
    echo "usage: $0 [-v]"
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

docker \
    run \
    -i \
    --rm \
    --mount type=bind,src=$REPO_DIR,dst=/repo \
    --user root \
    --env TARGET_UID=$(id -u) \
    --env TARGET_GID=$(id -g) \
    --entrypoint /repo/scripts/compile-udx-examples.sh \
    $VERTICA_IMG
