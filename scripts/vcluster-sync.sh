#!/bin/bash

SOURCE_BRANCH=$1
TARGET_BRANCH=$2
GITHUB_CRED=$3

# Trigger the GitHub Vcluster workflow dispatch event
curl -X POST \
 -H "Accept: application/vnd.github+json" \
 -H "Authorization: token ${GITHUB_CRED}" \
 --fail \
 https://api.github.com/repos/vertica/vcluster/actions/workflows/merge_branch.yml/dispatches \
 -d '{
 "ref":"'"${SOURCE_BRANCH}"'",
 "inputs":{
 "source_branch":"'"${SOURCE_BRANCH}"'",
 "target_branch":"'"${TARGET_BRANCH}"'"
 }
 }'