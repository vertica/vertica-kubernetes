#!/bin/bash

# (c) Copyright [2023-2024] Open Text.
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

# Run this script to sync the vclusterOps library in the vertica server repo
# into a separate git repo

set -o errexit
set -o pipefail
set -o nounset

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )"
FORCE=
SKIP_COMMIT=
DRY_RUN_OPT=

source $SCRIPT_DIR/logging-utils.sh

function usage() {
    echo "usage: $0 [-vfsd] <source> <dest>"
    echo
    echo "Optional Arguments:"
    echo " -f         Force, even when there are uncommitted files."
    echo " -s         Skip git commit at the destination. Just sync the files."
    echo " -d         Dry run only. Don't change anything in the destination repo."
    echo " -v         Verbose output."
    echo
    echo "Positional Arguments:"
    echo " <source>   The source directory of the vcluster ops. This should be"
    echo "            the directory in the server repo at platform/vcluster."
    echo " <dest>     The local destination directory where the standalone repo lives."
    exit 1
}

while getopts "hfsdv" opt
do
    case $opt in
      h) usage;;
      f) FORCE=1;;
      v) set -o xtrace;;
      s) SKIP_COMMIT=1;;
      d) DRY_RUN_OPT="--dry-run"
         SKIP_COMMIT=1
          ;;
    esac
done

if [ $(( $# - $OPTIND )) -lt 1 ]
then
    logError "Source and destination directories are required arguments"
    usage
fi

SOURCE_DIR=$(realpath ${@:$OPTIND:1})
DEST_DIR=$(realpath ${@:$OPTIND+1:1})

logInfo "Syncing vcluster from $SOURCE_DIR to $DEST_DIR"

# Some basic sanity to verify the destination is correct. It should be the
# top-level directory in a standalone git repository. 
DEST_GITROOT=$(cd $DEST_DIR && git rev-parse --show-toplevel 2>/dev/null || true)
logInfo "Destination directory git root directory: $DEST_GITROOT"
if [[ -z "$DEST_GITROOT" ]]
then
    logError "Destination directory isn't a git repo"
    exit 1
fi
if [[ "$DEST_GITROOT" != "$DEST_DIR" ]]
then
    logError "Destination directory isn't git's root directory"
    exit 1
fi

# To make sure the gitRef in the commit message includes all of the vcluster
# changes, we disallow running this command if files in the vcluster directory
# are not committed.
UNCOMMITTED_FILES=$(git status --porcelain | grep platform/vcluster | wc -l || true)
if [[ $UNCOMMITTED_FILES != 0 ]]
then
    msg="Uncommitted files found. Commit these changes to ensure a valid gitRef is used in the commit message."
    if  [[ -n "$FORCE" ]]
    then
        logWarning $msg
    else
        logError $msg
        exit 1
    fi
fi

logAndRunCommand "rsync $DRY_RUN_OPT --archive \
                                     --verbose \
                                     --delete \
                                     --exclude .git \
                                     --exclude vendor \
                                     --exclude bin \
                                     --exclude coverage.out \
                                     --exclude README.third-party.md \
                                     $SOURCE_DIR/ $DEST_DIR"

if [[ -n "$SKIP_COMMIT" ]]
then
    logWarning "Skipping git commit of sync'ed files"
    exit 0
fi

SERVER_GITREF=$(git rev-parse --short HEAD)
logInfo "Changing directory to $DEST_DIR"
cd $DEST_DIR
logAndRunCommand "git add --all ."

MSG_FILE=$(mktemp git-commit-msg-XXXXX)
trap "rm $MSG_FILE" EXIT
echo "Sync from server repo ($SERVER_GITREF)" > $MSG_FILE
logAndRunCommand "git commit --file $MSG_FILE"
