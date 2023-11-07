#!/bin/bash

set -o errexit
set -o xtrace

# v1 images only have sudo and no root password. v2 images don't have sudo but
# set a hard coded password for root. This scripts works for both images.
if which sudo  # v1 image
then
    for i in $(seq 1 5)
    do
       sudo apt-get update && sudo apt-get install -y libboost-all-dev libcurl4-openssl-dev libbz2-dev bzip2 && break || sleep 60
   done
else  # v2 image
    for i in $(seq 1 5)
    do
        echo root | su root sh -c 'apt-get update && apt-get install -y libboost-all-dev libcurl4-openssl-dev libbz2-dev bzip2' && break || sleep 60
    done
fi
