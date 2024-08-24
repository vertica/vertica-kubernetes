#!/bin/bash

set -o errexit
set -o xtrace

FOR_GITHUB_CI=$1

for i in $(seq 1 5)
do
    if [ "${FOR_GITHUB_CI:-false}" = "true" ]
    then
        echo "packages should already be installed for e2e tests"
        break
    # v1 images only have sudo and no root password. v2 images don't have sudo but
    # set a hard coded password for root. This scripts works for both images.
    elif which sudo  # v1 image
    then
        if which apt-get # ubuntu / apt-get
        then
           echo "Nothing for ubuntu images"
        else # yum package manager
           sudo yum install -y diffutils perl && break || sleep 60
        fi
    else  # v2 image
        if which apt-get # ubuntu / apt-get
        then
           echo "Nothing for ubuntu images"
        else # yum package manager
            echo root | su root sh -c 'yum install -y diffutils perl' && break || sleep 60
        fi
    fi
done
