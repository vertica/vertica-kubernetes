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

# A script that will pull down the vertica server images from our private repo
# and post it to the public repo. It is assumed the caller has logged into
# their docker account already.

set -o errexit
set -o pipefail

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )"
REPO_DIR=$(dirname $SCRIPT_DIR)
DRY_RUN=

source $SCRIPT_DIR/logging-utils.sh

function usage() {
    echo "usage: $0 [-vd] <server-version>"
    echo
    echo "Optional Arguments:"
    echo " -v            Verbose output"
    echo " -d            Dry run, don't actually push the new images"
    echo
    echo "Positional Arguments:"
    echo " <operator-version>   The operator version we are downloading artifacts for."
    echo "                      This must include the hotfix version (e.g. 12.0.3-0)."
    exit 1
}

while getopts "hvdc" opt
do
    case $opt in
      h) usage;;
      v) set -o xtrace;;
      d) DRY_RUN=1;;
    esac
done

if [ $(( $# - $OPTIND )) -lt 0 ]
then
    logError "Server version is a required argument"
    usage
fi

VERSION=${@:$OPTIND:1}

PRIV_REPO=vertica
PRIV_K8S_IMAGE=vertica-k8s-private
PRIV_CE_IMAGE=vertica-ce-private
PUB_REPO=vertica
PUB_K8S_IMAGE=vertica-k8s
PUB_CE_IMAGE=vertica-ce
logInfo "Pulling from private repo"
logAndRunCommand docker pull $PRIV_REPO/$PRIV_K8S_IMAGE:$VERSION
logAndRunCommand docker pull $PRIV_REPO/$PRIV_K8S_IMAGE:$VERSION-minimal
logAndRunCommand docker pull $PRIV_REPO/$PRIV_CE_IMAGE:$VERSION
logInfo "Show vertica versions in the container"
logAndRunCommand docker run --entrypoint /opt/vertica/bin/vertica $PRIV_REPO/$PRIV_K8S_IMAGE:$VERSION --version
logAndRunCommand docker run --entrypoint /opt/vertica/bin/vertica $PRIV_REPO/$PRIV_K8S_IMAGE:$VERSION-minimal --version
logInfo "Retag for public repo"
logAndRunCommand docker tag $PRIV_REPO/$PRIV_K8S_IMAGE:$VERSION $PUB_REPO/$PUB_K8S_IMAGE:$VERSION
logAndRunCommand docker tag $PRIV_REPO/$PRIV_K8S_IMAGE:$VERSION-minimal $PUB_REPO/$PUB_K8S_IMAGE:$VERSION-minimal
logAndRunCommand docker tag $PRIV_REPO/$PRIV_K8S_IMAGE:$VERSION-minimal $PUB_REPO/$PUB_K8S_IMAGE:latest
logAndRunCommand docker tag $PRIV_REPO/$PRIV_CE_IMAGE:$VERSION $PUB_REPO/$PUB_CE_IMAGE:$VERSION
logAndRunCommand docker tag $PRIV_REPO/$PRIV_CE_IMAGE:$VERSION $PUB_REPO/$PUB_CE_IMAGE:latest
if [[ -z "$DRY_RUN" ]]
then
    logInfo "Push to public repo"
    logAndRunCommand docker push $PUB_REPO/$PUB_K8S_IMAGE:$VERSION
    logAndRunCommand docker push $PUB_REPO/$PUB_K8S_IMAGE:$VERSION-minimal
    logAndRunCommand docker push $PUB_REPO/$PUB_K8S_IMAGE:latest
    logAndRunCommand docker push $PUB_REPO/$PUB_CE_IMAGE:$VERSION
    logAndRunCommand docker push $PUB_REPO/$PUB_CE_IMAGE:latest
else
    logWarning "Skipping the push to the public repo due to command line argument"
fi
