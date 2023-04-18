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

# A script that will show completed e2e runs that were initiated through a web
# dispatch (i.e. REST call).

set -o errexit
set -o pipefail

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )"
REPO_DIR=$(dirname $SCRIPT_DIR)

source $SCRIPT_DIR/logging-utils.sh

logInfo "Query GitHub to find CI runs"
tmpfile=$(mktemp /tmp/workflow-XXXXX.json)
trap "rm $tmpfile" EXIT
JQ_QUERY='[.[] | select (.event == "workflow_dispatch") | select (.status == "completed") | select (.workflowName == "e2e tests")]'
gh run list --branch main --json conclusion,event,status,url,displayTitle,workflowName -q "$JQ_QUERY" --limit 50 > $tmpfile
jq < $tmpfile
