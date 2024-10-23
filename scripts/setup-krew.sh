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

# A script that download krew locally and set it up

set -o xtrace
set -o errexit
set -o pipefail

KREW_URL=https://github.com/kubernetes-sigs/krew/releases/download/v0.4.1/krew.tar.gz

cd "$(mktemp -d)"
OS="$(uname | tr '[:upper:]' '[:lower:]')" &&
ARCH="$(uname -m | sed -e 's/x86_64/amd64/' -e 's/\(arm\)\(64\)\?.*/\1\2/' -e 's/aarch64$/arm64/')" &&
curl --retry 10 --retry-max-time 1800 -fsSLO $KREW_URL &&
tar zxvf krew.tar.gz &&
KREW=./krew-"${OS}_${ARCH}" &&
"$KREW" install krew
