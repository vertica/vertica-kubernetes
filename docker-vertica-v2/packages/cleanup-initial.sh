#!/bin/sh

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

# Remove things not necessary for a non-interactive Kubernetes pod
# running Vertica

# removing ssh related files
rm -rf \
    /var/lib/dpkg/info/libssh-4* \
    /usr/share/doc/libssh-4* \
    /usr/lib/x86_64-linux-gnu/libssh* \
    /usr/lib/apt/methods/ssh \
    /etc/X11/Xsession.d/90x11-common_ssh-agent \
    /usr/share/lintian/overrides/libssh-4