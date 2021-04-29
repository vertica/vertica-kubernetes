#!/bin/sh

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

# Remove things not necessary for a non-interactive Kubernetes pod
# running Vertica

# This script is hand-crafted:
# Build an image named bigimage
# 
#     docker run -it --entrypoint /bin/bash bigimage
#
# wander around in the image looking for things you can remove
rm -r -f \
   /anaconda-post.log \
   /usr/lib64/python2.7 \
   /usr/lib/python2.7 \
   /usr/bin/python2.7 \
   /usr/include/python2.7 \
   /usr/include/python2.7/pyconfig-64.h \
   /usr/lib64/libpython2.7.so.1.0 \
   /usr/share/systemtap/tapset/libpython2.7-64.stp \
   /opt/vertica/examples \
   /opt/vertica/sdk \
   /opt/vertica/packages/*/examples \
   /opt/vertica/oss/python3/lib/python3.7/test \
   /opt/vertica/oss/python3/lib/python3.7/pip \
   /opt/vertica/oss/python3/lib/python3.7/site-packages/pip \
   /opt/vertica/oss/python3/lib/python3.7/config-3.7*

   
# many of these directories contain things that aren't binaries
# thus divert error output to /dev/null
strip /opt/vertica/bin/* 2> /dev/null
strip /opt/vertica/lib/*.so*
strip /opt/vertica/oss/python3/bin/* 2> /dev/null
strip /opt/vertica/oss/python3/lib/libpython*.a
strip /opt/vertica/oss/python3/lib/python3.7/lib-dynload/*.so*

# stripping the packages directory saves about 900MB, but...
strip /opt/vertica/packages/*/lib/*.so* 2> /dev/null
# it changes the checksums used to verify the libraries when loaded
/opt/vertica/oss/python3/bin/python3 \
    /tmp/package-checksum-patcher.py /opt/vertica/packages/*
