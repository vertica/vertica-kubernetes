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

# Wait for the cert-manager for webhook to be ready
# config/samples/test-cert-manager.yaml is used from: https://cert-manager.io/docs/installation/kubernetes/#verifying-the-installation


TIMEOUT=30  # Default, can be overridden

function usage() {
    echo "usage: $(basename $0) [-t <timeout>]"
    echo
    echo "Options:"
    echo "  -t    Timeout in seconds.  Defaults to $TIMEOUT"
    echo
    exit 1
}

while getopts ":ht:" opt
do
    case $opt in
        t)
            TIMEOUT=$OPTARG
            ;;
        h)
            usage
            ;;
        \?)
            echo "ERROR: unrecognized option: $opt"
            usage
            ;;
    esac
done

ERR_MSG="Init error message"

START_TIME="$(date -u +%s)"
while [[ $ERR_MSG != '' ]]; do
    END_TIME="$(date -u +%s)"
    ELAPSED="$(($END_TIME-$START_TIME))"
    if [[ "$ELAPSED" -gt "$TIMEOUT" ]]; then
        echo $START_TIME
        echo $END_TIME
        echo $ELAPSED
        echo "Timed out waiting for cert-manager ready."
        exit 1
    fi
    sleep 1
    if kubectl apply -f config/samples/test-cert-manager.yaml 2>&1 1>/dev/null
    then
        break
    fi
done
