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

# This script will patch the given vdb.  It expects the patch to fail.  A regex
# is given that must match the error message.  If the patch fails with the
# expected message, then the script exits with a return code of 0.

set -o errexit

function usage() {
    echo "usage: $(basename $0) [-n <namespace>] <vdb_name> <patch> <errorRegEx>"
    echo
    echo "Options:"
    echo "  -n <namespace>  Namespace of the vdb object"
    echo
    echo "This script is used to verify a patch failure.  It will return 0 "
    echo "if the patch returns the expected error."
    exit 1
}

while getopts "hn:" opt
do
    case $opt in
        h) 
            usage
            ;;
        n)
            NS_OPT="-n $OPTARG"
            ;;
        \?)
            echo "ERROR: unrecognized option: -$opt"
            usage
            ;;
    esac
done
shift "$((OPTIND-1))"

if [ "$#" -ne 3 ]; then
    echo "expecting exactly 3 positional arguments"
    usage
fi

VDB_NAME=$1
PATCH=$2
ERROR_REGEX=$3

tmpfile=$(mktemp /tmp/patch-XXXXXX.yaml)
trap "rm $tmpfile" 0 2 3 15   # Ensure deletion on script exit
cat <<- EOF > $tmpfile
$PATCH
EOF

CMD="kubectl patch vdb $VDB_NAME $NS_OPT --type=merge --patch-file $tmpfile"
echo $CMD
set +o errexit
KUBECTL_OP=$($CMD 2>&1)
KUBECTL_RES=$?
set -o errexit

echo "Result is: $KUBECTL_RES"
echo "Output is: $KUBECTL_OP"

if [ "$KUBECTL_RES" -eq 0 ]
then
  echo "patch command succeeded instead of failing"
  exit 1
fi

if [[ $KUBECTL_OP =~ $ERROR_REGEX ]]
then
  echo "error regex matches"
  exit 0
fi

echo "error regex does not match"
exit 1
