#!/bin/bash

# (c) Copyright [2021] Micro Focus or one of its affiliates.
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

# A script that will run the integration tests in a loop until it fails.

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )"
REPO_DIR=$(dirname $SCRIPT_DIR)
cd $REPO_DIR # Change to the root of the repository

function usage() {
    echo "usage: $(basename $0) [-t <testcase>]"
    echo
    echo "Options:"
    echo "  -t <testcase>   Run only the following testcase.  By default it"
    echo "                  runs all of the testcases."
    exit 1
}

while getopts "t:h" opt
do
    case $opt in
        t)
            TESTCASE="--test $OPTARG"
            ;;
        h) 
            usage
            ;;
        \?)
            echo "ERROR: unrecognized option: -$opt"
            usage
            ;;
    esac
done

function start_iteration {
    printf "\n${GREEN}Iteration $(($count))${NC}\n"
    printf "\t${ORANGE}$(date +%r): Deleting old kuttl namespaces${NC}\n"
    for ns in $(kubens | grep kuttl)
    do
        kubectl delete ns $ns 2>&1 > /dev/null
    done
}

RED='\033[0;31m'
GREEN='\033[0;32m'
ORANGE='\033[0;33m'
YELLOW='\033[1;33m'
NC='\033[0m'  # No color

OP=$REPO_DIR/int-tests-output/kuttl.out
mkdir -p $(dirname $OP)
printf "${YELLOW}$(date +%r): Logging output to: $OP${NC}\n\n"
count=1
start_iteration
trap "printf \"${RED}$(date +%r): *** Failed${NC}\n\"; set -o xtrace; tail -20 $OP" EXIT
while printf "\t${ORANGE}$(date +%r): Starting kuttl${NC}\n" && kubectl kuttl test $TESTCASE --skip-delete > $OP 2>&1
do
    (( count++ ))
    start_iteration
done
