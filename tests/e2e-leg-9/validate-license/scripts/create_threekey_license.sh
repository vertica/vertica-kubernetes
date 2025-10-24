#!/bin/sh
set -x
echo "$1 $2"
license_content=$(cat $LICENSE_FILE)
kubectl create secret generic ${1} --from-file=invalid=${2} --from-file=ce=${3} --from-literal=valid="${license_content}" --namespace $NAMESPACE