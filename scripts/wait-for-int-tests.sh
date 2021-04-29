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

while getopts "q" opt
do
    case $opt in
        q) QUIET=1;;
    esac
done

TESTSUITE_NAME=$1
if [[ -n TESTSUITE_NAME ]]
then
    TESTSUITE_NAME="testsuite-sanity"
fi

DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" >/dev/null 2>&1 && pwd )"
if [ $(kubectl get cts | wc -l 2> /dev/null) -lt 2 ]
then
    exit 1
fi

show_status_if_verbose () {
    if [[ -z $QUIET ]]
    then
        clear
        kubectl get cts $TESTSUITE_NAME -ogo-template-file --template=$DIR/suite.template
    fi
}

while :
do
    show_status_if_verbose
    kubectl wait --for=condition=Succeeded --timeout=2s cts $TESTSUITE_NAME 2> /dev/null 1>&2
    if [ $? -eq 0 ]
    then
        show_status_if_verbose
        exit 0
    fi

    kubectl wait --for=condition=Failed --timeout=2s cts $TESTSUITE_NAME 2> /dev/null 1>&2
    if [ $? -eq 0 ]
    then
        show_status_if_verbose
        echo " *** Test suite failed"
        exit 1
    fi
done
