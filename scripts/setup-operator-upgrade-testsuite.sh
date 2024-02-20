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

# Setup the e2e operator upgrade test suite.  This test suite will upgrade the
# operator from a fixed version to the current version.  All tests are
# generally the same, so we use a template and then copy-in the steps that are
# specific to a given operator version.  This script will setup the tests.
# The new tests will be in tests/e2e-operator-upgrade-overlays/

set -o errexit
set -o pipefail

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )"
REPO_DIR=$(dirname $SCRIPT_DIR)
VERBOSE=

source $SCRIPT_DIR/logging-utils.sh

function usage {
    echo "usage: $0 [-hv]"
    echo
    echo "Refresh the tests in e2e-operator-upgrade-overlays"
    echo
    echo "Options:"
    echo "  -v                 Verbose output"
    echo
    exit 1
}

OPTIND=1
while getopts "hv" opt; do
    case ${opt} in
        h)
            usage
            ;;
        v)
            set -o xtrace
            VERBOSE=1
            ;;
        \?)
            echo "Unknown option: -${opt}"
            usage
            ;;
    esac
done

TEMPLATE_DIR=$REPO_DIR/tests/e2e-operator-upgrade-template/
OVERLAY_DIR=$REPO_DIR/tests/e2e-operator-upgrade-overlays/
logInfo "Refresh testcases in $OVERLAY_DIR"
logInfo "Removing old overlays"
git clean -d --force -x $OVERLAY_DIR

logInfo "Create new overlays"
cd $TEMPLATE_DIR
for tdir in *
do
    # Skip if not a directory or the special template directory.
    if ! test -d $tdir || [ "$tdir" == "template" ]
    then
        continue
    fi
    logInfo "Refresh testcase $tdir"
    OVERLAY_TDIR=$OVERLAY_DIR/$tdir
    mkdir $OVERLAY_TDIR
    cp -r template/* $OVERLAY_TDIR
    cp -r $tdir/* $OVERLAY_TDIR
done
