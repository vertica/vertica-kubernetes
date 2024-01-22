#!/bin/bash

set -o errexit
set -o xtrace

for i in $(seq 1 5)
do
    # v1 images only have sudo and no root password. v2 images don't have sudo but
    # set a hard coded password for root. This scripts works for both images.
    if which sudo  # v1 image
    then
        if which apt-get # ubuntu / apt-get
        then
           sudo apt-get update && sudo apt-get install -y libboost-all-dev libcurl4-openssl-dev libbz2-dev bzip2 perl && break || sleep 60
        else # yum package manager
           # The GitHub hosted runners are ubuntu, so we need to account for some OS differences.
           # - /usr/lib64/libbz2.so.1.0 exists on ubuntu and is a shared
           #   library in the test-verify-cpp-filter test
           # - test-verify-cpp-apportion-load depends on python being in the
           #   container. python3 is there, so just creating a symlink.
           sudo yum install -y boost-devel libcurl-devel bzip2-devel bzip2 perl diffutils && sudo ln -s /opt/vertica/oss/python3/bin/python3 /usr/bin/python && sudo ln -s /usr/lib64/libbz2.so /usr/lib64/libbz2.so.1.0 && break || sleep 60
        fi
    else  # v2 image
        if which apt-get # ubuntu / apt-get
        then
            echo root | su root sh -c 'apt-get update && apt-get install -y libboost-all-dev libcurl4-openssl-dev libbz2-dev bzip2 perl' && break || sleep 60
        else # yum package manager
            # The GitHub hosted runners are ubuntu, so we need to account for
            # some OS differences. See comment above to understand rationale.
            echo root | su root sh -c 'yum install -y boost-devel libcurl-devel bzip2-devel bzip2 perl diffutils && ln -s /opt/vertica/oss/python3/bin/python3 /usr/bin/python && ln -s /usr/lib64/libbz2.so /usr/lib64/libbz2.so.1.0' && break || sleep 60
        fi
    fi
done
