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

# A script that will be used to test logging to file.

cat <<EOF > test-logging-patch.yaml
spec:
  template:
    spec:
      containers:
      - name: manager
        volumeMounts:
        - mountPath: /logs
          name: shared-data
      - args:
        - -c
        - while true; do tail -n 1 /var/logs/try.log; sleep 5;done
        command:
        - /bin/sh
        image: busybox
        name: sidecar-container
        securityContext:
          runAsUser: 65532
        volumeMounts:
        - mountPath: /var/logs
          name: shared-data
      volumes:
      - emptyDir: {}
        name: shared-data
EOF