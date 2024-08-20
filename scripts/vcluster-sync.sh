#!/bin/bash

SOURCE_BRANCH=$1
TARGET_BRANCH=$2
GITHUB_CRED=$3
GITHUB_OWNER=vertica
GITHUB_REPO=vcluster

curl -L -v \
  -X POST \
  -H "Accept: application/vnd.github+json" \
  -H "Authorization: Bearer $GITHUB_CRED" \
  -H "X-GitHub-Api-Version: 2022-11-28" \
  https://api.github.com/repos/$GITHUB_OWNER/$GITHUB_REPO/dispatches \
  -d '{"event_type": "curl_request_merge", "client_payload":{"source_branch":"'"$SOURCE_BRANCH"'","target_branch":"'"$TARGET_BRANCH"'"}}'