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

# A script that will increase the operator version in the repo. After the
# script is run, it's up to the caller to verify and commit the changes to git.

set -o errexit
set -o pipefail

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )"
REPO_DIR=$(dirname $SCRIPT_DIR)

source $SCRIPT_DIR/logging-utils.sh

function usage() {
    echo "usage: $0 [-v] <new-version>"
    echo
    echo "Optional Arguments:"
    echo " -v                      Verbose output"
    echo
    echo "Positional Arguments:"
    echo " <new-version>           New operator version (e.g. 1.8.0)"
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
    logError "Missing positional arguments"
    usage
fi

VERSION=${@:$OPTIND:1}

logInfo "Moving to new version: $VERSION"

logInfo "Changing Makefile"
perl -i -0777 -pe "s/^(VERSION \?= ).*/\${1}$VERSION/gm" $REPO_DIR/Makefile

logInfo "Changing version in the operator controller"
perl -i -0777 -pe "s/(CurOperatorVersion = \")[0-9\.]+(\")/\${1}$VERSION\${2}/g" $REPO_DIR/pkg/meta/labels.go
cd $REPO_DIR
make fmt

logInfo "Changing version in the helm chart"
perl -i -0777 -pe "s/(name: .*verticadb-operator:)[0-9\.]+/\${1}$VERSION/g" $REPO_DIR/helm-charts/verticadb-operator/values.yaml
perl -i -0777 -pe "s/^(version: )[0-9\.]+/\${1}$VERSION/gm" $REPO_DIR/helm-charts/verticadb-operator/Chart.yaml
perl -i -0777 -pe "s/(verticadb-operator:)[0-9\.]+/\${1}$VERSION/g" $REPO_DIR/helm-charts/verticadb-operator/README.md
