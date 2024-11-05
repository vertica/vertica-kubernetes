#!/bin/bash
# TODO: put vertica-client-proxy image into dockerhub
CURDIR=$(pwd)
TMPDIR=$(mktemp -d)
cd $TMPDIR
git clone git@github.com:vertica/vertica-client-proxy.git
cd vertica-client-proxy
make docker-image
cd $CURDIR
rm -rf $TMPDIR
