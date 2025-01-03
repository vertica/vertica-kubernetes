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

# Utilities to be sourced into various bash scripts

CYAN='\033[0;36m'
RED='\033[1;31m'
ORANGE='\033[0;33m'
GREEN='\033[1;32m'
NC='\033[0m'  # No color

function logGeneric {
    printf $1
    shift
    printf $(date "+%D")
    printf " "
    printf $(date "+%T")
    printf " [$1]  "
    shift
    for i in $*; do
        printf -- "$i "
    done
    printf "$NC\n"

}

function logInfo {
    logGeneric ${CYAN} "ℹ" $@
}

function logError {
    logGeneric ${RED} "💀" $@
}

function logWarning {
    logGeneric ${ORANGE} "⚠" $@
}

function logAndRunCommand {
    logGeneric ${GREEN} "🕹️ " $@
    $@
}

