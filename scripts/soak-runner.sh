#!/bin/bash

# (c) Copyright [2021-2024] Open Text
#
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

ITERATIONS=1

source $SCRIPT_DIR/logging-utils.sh

function usage() {
    echo "usage: $(basename $0) [-v] [-i <#>] <config_file>"
    echo
    echo "Options:"
    echo "  -i <#>     Number of iterations to run. If this is a negative"
    echo "             number, we run an infinite number of iterations. Default $ITERATIONS."
    echo "  -v         Verbose output"
    exit 1
}

while getopts "i:hv" opt
do
    case $opt in
        i)
            ITERATIONS=$OPTARG
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
shift "$((OPTIND-1))"

if [ "$#" -lt 1 ]; then
    echo "expecting at least 1 positional arguments"
    usage
fi

CONFIG_FILE=$1

KUTTL_OUT="int-tests-output/soak.out"
KUTTL_STEP_GEN_OUT="int-tests-output/kuttl_step_gen.out"
STEP_OUTPUT_DIR="./tests/soak/steps"

mkdir -p int-tests-output
rm $KUTTL_OUT 2> /dev/null || :
rm $KUTTL_STEP_GEN_OUT 2> /dev/null || :

ITERATIONS_STR=$ITERATIONS
if [[ $ITERATIONS -lt 0 ]]
then
  ITERATIONS_STR="infinite"
fi

logInfo "Running $ITERATIONS_STR iterations of $TEST_STEPS steps"

tmpfile=/tmp/local-soak.cfg
cp $CONFIG_FILE $tmpfile
trap "rm $tmpfile" 0 2 3 15  # Ensure deletion on script exit

curIter=0
until [[ $curIter -eq $ITERATIONS ]]
do
    logInfo "Iteration $((curIter+1))"

    # Generate the kuttl test steps for this iteration
    logInfo "\tGenerating test steps. Appending output to $KUTTL_STEP_GEN_OUT"
    bin/kuttl-step-gen --output-dir=$STEP_OUTPUT_DIR --scripts-dir="../../../scripts" 2>> $KUTTL_STEP_GEN_OUT

    KUTTL_CFG="kuttl-soak-test-iteration.yaml"
    logInfo "\tRunning kuttl.  Appending output to $KUTTL_OUT"
    trap "logError \"Failed. End of test run:\"; tail $KUTTL_OUT" EXIT
    kubectl kuttl test --config $KUTTL_CFG >> $KUTTL_OUT
    trap "" EXIT
    ((curIter=curIter+1))
done

logInfo "All iterations done"
