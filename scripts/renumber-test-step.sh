#!/bin/bash

# (c) Copyright [2021-2022] Micro Focus or one of its affiliates.
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

# A script that will renumber a step in a kuttl testcase

set -o errexit
set -o pipefail

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )"
REPO_DIR=$(dirname $SCRIPT_DIR)

function usage() {
    echo "usage: $0 [-v] <testsuite> <testcase> <old-number> <new-number>"
    echo
    echo "Positional Arguments:"
    echo " <testsuite>   The name of the testsuite (e.g. e2e-leg-1)"
    echo " <testcase>    The name of the testcase in the testsuite to rename"
    echo " <old-number>  Old step number to rename"
    echo " <new-number>  Number to use for new step number"
    echo
    echo "Optional Arguments:"
    echo " -v            Show verbose output"
    exit 1
}

while getopts "hv" opt
do
    case $opt in
      h) usage;;
      v) set -o xtrace;;
    esac
done

if [ $(( $# - $OPTIND )) -lt 3 ]
then
    usage
fi

TESTSUITE=${@:$OPTIND:1}
TESTCASE=${@:$OPTIND+1:1}
OLD_NUMBER=${@:$OPTIND+2:1}
NEW_NUMBER=${@:$OPTIND+3:1}

echo "$TESTSUITE.$TESTCASE moving all steps starting with $OLD_NUMBER to $NEW_NUMBER"

test -d $REPO_DIR/tests/$TESTSUITE/$TESTCASE
rename -v "s/$OLD_NUMBER/$NEW_NUMBER/g" $(ls $REPO_DIR/tests/$TESTSUITE/$TESTCASE/$OLD_NUMBER-*)
