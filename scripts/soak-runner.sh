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

set -o errexit

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )"
cd $SCRIPT_DIR/..  # Change to the root of the repository

function usage() {
    echo "usage: $(basename $0) [-v] [-c <config_file>]"
    echo
    echo "Options:"
    echo "  -c <config_file>   Read the following as a config file for the soak run."
    echo "                     The config file is sourced in and overrides any of the defaults."
    echo "  -v                 Verbose output"
    exit 1
}

while getopts "c:hv" opt
do
    case $opt in
        c)
            CONFIG=$OPTARG
            ;;
        h) 
            usage
            ;;
        v)
            set -o xtrace
            ;;
        \?)
            echo "ERROR: unrecognized option: -$opt"
            usage
            ;;
    esac
done

# Read in the defaults
source tests/soak/soak-defaults.cfg

# Override any of the defaults from the users config file if provided.
if [ -n "$CONFIG" ]
then
    source $CONFIG
fi

# Read in the kustomize defaults for setup-kustomize.sh
source tests/kustomize-defaults.cfg
if [ -n "$KUSTOMIZE_CFG" ]
then
    source $KUSTOMIZE_CFG
fi

RED='\033[0;31m'
GREEN='\033[0;32m'
ORANGE='\033[0;33m'
NC='\033[0m'  # No color
NAMESPACE=soak
KUTTL_OUT="int-tests-output/soak.out"
STEP_OUTPUT_DIR="./tests/soak/steps"

rm $KUTTL_OUT 2> /dev/null || :
kubectl delete namespace $NAMESPACE 2> /dev/null || :
kubectl create namespace $NAMESPACE

# Install the license file if one exists.
if [ -n "$LICENSE_FILE" && -n "$LICENSE_SECRET" ]
then
    kubectl -n $NAMESPACE create secret generic $LICENSE_SECRET --from-file=license.key=$LICENSE_FILE
fi

# NOTE: Most of the environment variables in use here were read in from the config file.
printf "${ORANGE}Running $ITERATIONS iterations of $TEST_STEPS steps.${NC}\n"

for (( i=0; i != $ITERATIONS; i++ ))
do
    printf "\n${GREEN}Iteration $(($i+1))${NC}\n"

    # Generate the kuttl test steps for this iteration
    printf "\t${ORANGE}Generating test steps${NC}\n"
    bin/kuttl-step-gen \
      -script-dir ../../../scripts \
      -min-pods-to-kill $MIN_PODS_TO_KILL \
      -max-pods-to-kill $MAX_PODS_TO_KILL \
      -min-sleep-time $MIN_SLEEP_TIME \
      -max-sleep-time $MAX_SLEEP_TIME \
      -max-subclusters $MAX_SUBCLUSTERS \
      -min-subclusters $MIN_SUBCLUSTERS \
      -max-pods $MAX_PODS \
      -min-pods $MIN_PODS \
      -steady-state-timeout $STEADY_STATE_TIMEOUT \
      $TEST_STEPS \
      $STEP_OUTPUT_DIR

    if [ "$i" -eq "0" ]
    then
        KUTTL_CFG="kuttl-soak-test-iteration-0.yaml"
    else
        KUTTL_CFG="kuttl-soak-test-iteration-n.yaml"
    fi
    printf "\t${ORANGE}Running kuttl.  Appending output to $KUTTL_OUT${NC}\n"
    trap "printf \"${RED}*** Failed${NC}\n\"; set -o xtrace; tail $KUTTL_OUT" EXIT
    kubectl kuttl test --config $KUTTL_CFG >> $KUTTL_OUT
done

trap "" EXIT
printf "\n${GREEN}All iterations done.${NC}\n"
printf "\t${ORANGE}Cleaning up namespace\n${NC}"
kubectl delete namespace $NAMESPACE
