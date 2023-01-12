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

# A script that will create a draft GitHub release for the given version of the
# operator. It is expected that the release tag already exist.

set -o errexit
set -o pipefail

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )"
REPO_DIR=$(dirname $SCRIPT_DIR)
ARTIFACTS_DIR=$REPO_DIR/ci-artifacts

source $SCRIPT_DIR/logging-utils.sh

function usage() {
    echo "usage: $0 [-v] [-d <directory>] <operator-version>"
    echo
    echo "Optional Arguments:"
    echo " -v                   Verbose output"
    echo " -d <directory>       The base directory to find the artifacts. The actual directory"
    echo "                      will include the version number [default: $ARTIFACTS_DIR]"
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
    esac
done

if [ $(( $# - $OPTIND )) -lt 0 ]
then
    logError "Operator version is a required argument"
    usage
fi

VERSION=${@:$OPTIND:1}

logInfo "Verify git tag exists for version ($VERSION)"
git tag --list v$VERSION
VERSION_SHA=$(git rev-list -n 1 v$VERSION)

logInfo "Verify artifacts exist"
ARTIFACTS_DIR="${ARTIFACTS_DIR}/${VERSION}/release-artifacts"
if [ -z "$(ls -A $ARTIFACTS_DIR)" ]
then
  logError "No artifacts found in $ARTIFACTS_DIR"
  exit 1
fi

logInfo "Verify versions' changelog exists"
CHANGELOG_FILE=$REPO_DIR/changes/${VERSION}.md
if [ ! -f "$CHANGELOG_FILE" ]
then
  logError "Could not find CHANGELOG file for version: $CHANGELOG_FILE"
  exit 1
fi
TMP_CHANGELOG_FILE=$(mktemp /tmp/rel-notes-XXXXX.txt)
trap "rm $TMP_CHANGELOG_FILE" EXIT
# Manipulate the changelog file so that it is suitable for the release notes.
tail -n +2 $CHANGELOG_FILE | sed 's/^###/##/' > $TMP_CHANGELOG_FILE

logInfo "Creating the release"
gh release create v$VERSION \
    --notes-file $TMP_CHANGELOG_FILE \
    --target $VERSION_SHA \
    --title "Vertica Kubernetes $VERSION" \
    $ARTIFACTS_DIR/*
