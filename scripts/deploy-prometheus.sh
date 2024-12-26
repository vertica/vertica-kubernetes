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

# A script that will deploy the prometheus service monitor and secret. It assumes the prometheus operator is deployed.


SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )"
REPO_DIR=$(dirname $SCRIPT_DIR)
LABEL=''
ACTION=''
NAMESPACE=''
USERNAME=''
PASSWORD=''
DBNAME=''

function usage() {
    echo "usage: $(basename $0) [-n <namespace>] [-l <label>] [-a <action>] [-u <username>] [-p <password>] [-d <dbname>]"
    echo
    echo "Options:"
    echo "  -n <namespace>  The namespace used for prometheus service."
    echo "  -l <label>      The label monitored by prometheus service."
    echo "  -a <action>     The action to run in this script, deploy or undeploy."
    echo "  -u <username>   The database username, should have access to the Vertica server metrics."
    echo "  -p <password>   The database user password."
    echo "  -d <dbname>     The database name."
    echo "  -h <usage>      Print help message."
    exit 1
}

while getopts "n:l:a:u:p:d:h" opt
do
    case $opt in
        n)
            NAMESPACE=$OPTARG
            ;;
        l)
            LABEL=$OPTARG
            ;;
        a)
            ACTION=$OPTARG
            ;;
        u)
            USERNAME=$OPTARG
            ;;
        p)
            PASSWORD=$OPTARG
            ;;
        d)
            DBNAME=$OPTARG
            ;;
        h) 
            usage
            exit 0
            ;;
        \?)
            echo "ERROR: unrecognized option: -$opt"
            usage
            exit 0
            ;;
    esac
done

if [ -z "$NAMESPACE" ]
then
  NAMESPACE=default
fi


function deploy(){
  # Create an secret to store prometheus service monitor database username and password.
  cat <<EOF | kubectl apply -f -
apiVersion: v1
kind: Secret
metadata:
  namespace: $NAMESPACE
  name: prometheus-$DBNAME
data:
  username: '$(echo -n $USERNAME | base64)'
  password: '$(echo -n $PASSWORD | base64)'
type: Opaque
EOF

  # Create a subscription to the verticadb-operator
  cat <<EOF | kubectl apply -f -
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: k8s-vertica-prometheus-$DBNAME
  labels:
    release: $LABEL
spec:
  selector:
    matchLabels:
      app.kubernetes.io/instance: $DBNAME
  namespaceSelector:
    matchNames:
      - $NAMESPACE
  endpoints:
    - basicAuth:
        password:
          key: password
          name: prometheus-$DBNAME
        username: 
          key: username
          name: prometheus-$DBNAME
          optional: true
      interval: 5s
      path: /v1/metrics
      port: vertica-http
      scheme: https
      tlsConfig:
        insecureSkipVerify: true
EOF
}

function undeploy(){
  # Delete the service monitor for prometheus service monitor.
  cat <<EOF | kubectl delete -f -
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: k8s-vertica-prometheus-$DBNAME
  labels:
    release: $LABEL
spec:
  selector:
    matchLabels:
      app.kubernetes.io/instance: $DBNAME
  namespaceSelector:
    matchNames:
      - $NAMESPACE
  endpoints:
    - basicAuth:
        password:
          key: password
          name: prometheus-$DBNAME
        username: 
          key: username
          name: prometheus-$DBNAME
          optional: true
      interval: 5s
      path: /v1/metrics
      port: vertica-http
      scheme: https
      tlsConfig:
        insecureSkipVerify: true
EOF

  # delete secret for prometheus service monitor.
  cat <<EOF | kubectl delete -f -
apiVersion: v1
kind: Secret
metadata:
  namespace: $NAMESPACE
  name: prometheus-$DBNAME
data:
  username: '$(echo -n $USERNAME | base64)'
  password: '$(echo -n $PASSWORD | base64)'
type: Opaque
EOF
}

 # ACTION deploy and undeploy
case $ACTION in
    deploy) 
        echo "Running task: $ACTION"
        deploy
        ;;
    undeploy) 
        echo "Running task: $ACTION"
        undeploy
        ;;
    *) 
        echo "Invalid action: '$ACTION'"
        usage
        ;;
esac
