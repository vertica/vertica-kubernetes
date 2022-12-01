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

# A script that will download the artifacts from the last good main build.

set -o errexit
set -o pipefail

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )"
REPO_DIR=$(dirname $SCRIPT_DIR)

source $SCRIPT_DIR/logging-utils.sh

function usage() {
    echo "usage: $0 [-v] <operator-version>"
    echo
    echo "Optional Arguments:"
    echo " -v                      Verbose output"
    echo
    echo "Positional Arguments:"
    echo " <operator-version>      The operator version we are downloading artifacts for"
    exit 1
}

while getopts "hv" opt
do
    case $opt in
      h) usage;;
      v) set -o xtrace;;
    esac
done

if [ $(( $# - $OPTIND )) -lt 0 ]
then
    logError "Operator version is a required argument"
    usage
fi

VERSION=${@:$OPTIND:1}

logInfo "Verify git tag exists for version ($VERSION)"
git tag --verify $VERSION
VERSION_SHA=$(git show-ref --hash $VERSION)

logInfo "Query GitHub to find CI run for release"
tmpfile=$(mktemp /tmp/workflow-XXXXX.json)
trap "rm $tmpfile" EXIT
JQ_QUERY='[.[] | select (.event == "push") | select (.headSha == "'"$VERSION_SHA"'")][0]'
logInfo $JQ_QUERY
gh run list --branch main --json conclusion,event,workflowDatabaseId,status,headSha,url -q "$JQ_QUERY" | tee $tmpfile

logInfo "Verify CI run is successful"
jq -e '. | select(.conclusion == "success")' < $tmpfile