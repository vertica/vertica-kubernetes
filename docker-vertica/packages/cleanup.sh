#!/bin/sh

# (c) Copyright [2021-2022] Open Text.
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

# This script is hand-crafted:
# Build an image named bigimage
# 
#     docker run -it --entrypoint /bin/bash bigimage
#
# wander around in the image looking for things you can remove
rm -r -f \
   /opt/vertica/config/https_certs/*.key \
   /opt/vertica/examples \
   /opt/vertica/packages/*/examples \
   /opt/vertica/oss/python*/lib/python*/test \
   /opt/vertica/oss/python*/lib/python*/unittest/test \
   /opt/vertica/oss/python*/lib/python*/pip \
   /opt/vertica/oss/python*/lib/python*/site-packages/pip \
   /opt/vertica/oss/python*/lib/python*/config-[0-9]* \
   /opt/vertica/oss/python*/lib/python*/tkinter \
   /opt/vertica/oss/python*/lib/python*/idlelib

# cleanup all test directories for packages under site-package
find /opt/vertica/oss/python*/lib/python*/site-packages/ -type d -name "*[Tt]est" -exec rm -rf {} +

# cleanup many of the __pycache__ directories 
find /opt/vertica/oss/ -type d -name "__pycache__" -exec rm -rf {} +
   
# many of these directories contain things that aren't binaries
# thus divert error output to /dev/null
strip /opt/vertica/bin/* 2> /dev/null
strip /opt/vertica/lib/*.so*
strip /opt/vertica/oss/python*/bin/* 2> /dev/null
strip /opt/vertica/oss/python*/lib/libpython*.a
strip /opt/vertica/oss/python*/lib/python*/lib-dynload/*.so*

# stripping the packages directory saves about 900MB, but...
strip /opt/vertica/packages/*/lib/*.so* 2> /dev/null
# it changes the checksums used to verify the libraries when loaded
/opt/vertica/oss/python*/bin/python[0-9] \
    /tmp/package-checksum-patcher.py /opt/vertica/packages/*

# (optional) minimal images remove packages that aren't auto installed as well as the sdk folder
if [ "$MINIMAL" = "YES" ] || [ "$MINIMAL" = "yes" ]
then 
  cd /opt/vertica/packages
  for i in $(find . -name package.conf -exec grep Autoinstall=False {} + | cut -d"/" -f2)
  do
   rm -rf $i
  done
  rm -r -f /opt/vertica/sdk
fi
