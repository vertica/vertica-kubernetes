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

# A script that will download the artifacts for a release build. The operator
# version must already be tagged and the e2e run must be successful.

set -o errexit
set -o pipefail

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )"
REPO_DIR=$(dirname $SCRIPT_DIR)
ARTIFACTS_DIR=$REPO_DIR/ci-artifacts
CLEAN_ARTIFACTS_DIR=

source $SCRIPT_DIR/logging-utils.sh

function usage() {
    echo "usage: $0 [-vc] [-d <directory>] <operator-version>"
    echo
    echo "Optional Arguments:"
    echo " -c                   Clean the directory prior to downloading the artifacts."
    echo " -d <directory>       The base directory to store the artifacts. The actual directory"
    echo "                      will include the version number [default: $ARTIFACTS_DIR]"
    echo " -v                   Verbose output"
    echo
    echo "Positional Arguments:"
    echo " <operator-version>   The operator version we are downloading artifacts for"
    exit 1
}

while getopts "hvd:c" opt
do
    case $opt in
      h) usage;;
      v) set -o xtrace;;
      d) ARTIFACTS_DIR=$OPTARG;;
      c) CLEAN_ARTIFACTS_DIR=1;;
    esac
done

if [ $(( $# - $OPTIND )) -lt 0 ]
then
    logError "Operator version is a required argument"
    usage
fi

VERSION=${@:$OPTIND:1}

logInfo "Verify git tag exists for version ($VERSION)"
cd $REPO_DIR
git tag --list v$VERSION
VERSION_SHA=$(git rev-list -n 1 v$VERSION)

logInfo "Query GitHub to find CI run for release"
tmpfile=$(mktemp /tmp/workflow-XXXXX.json)
trap "rm $tmpfile" EXIT
JQ_QUERY='[.[] | select (.event == "push") | select (.headSha == "'"$VERSION_SHA"'")][0]'
gh run list --branch main --json conclusion,event,databaseId,status,headSha,url -q "$JQ_QUERY" | tee $tmpfile

logInfo "Verify CI run is successful"
jq -e '. | select(.conclusion == "success")' < $tmpfile

logInfo "Preparing the artifacts directory"
ARTIFACTS_DIR="${ARTIFACTS_DIR}/${VERSION}"
mkdir -p $ARTIFACTS_DIR
if [ -n "$CLEAN_ARTIFACTS_DIR" ] && [ -n "$(ls -A $ARTIFACTS_DIR)" ]
then
  logWarning "Removing the contents of the artifacts directory"
  rm -r $ARTIFACTS_DIR/*
fi

DATABASE_ID=$(jq -r '.databaseId' < $tmpfile)
logInfo "Downloading artifacts for run ID $DATABASE_ID into $ARTIFACTS_DIR"
gh run download $DATABASE_ID --dir $ARTIFACTS_DIR --name release-artifacts --name olm-bundle
