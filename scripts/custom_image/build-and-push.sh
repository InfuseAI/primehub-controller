#!/bin/bash

readonly INIT_FAILED=1
readonly BUILD_FAILED=2
readonly PUSH_FAILED=3

readonly PUSH_SECRET_AUTHFILE=/push-secret/.dockerconfigjson
readonly PULL_SECRET_AUTHFILE=/pull-secret/.dockerconfigjson

function init() {
  echo "[Step: init]"
  echo "Generating Dockerfile"
  echo "$DOCKERFILE" > /Dockerfile
  [[ $? -ne 0 ]] && { exit $INIT_FAILED; }
  echo
}

function build() {
  local -a AUTFILE_FLAGS
  echo "[Step: build]"
  echo "Fetching base image: $BASE_IMAGE"
  if [ -f $PULL_SECRET_AUTHFILE ]; then
    AUTHFILE_FLAGS+=(--authfile "$PULL_SECRET_AUTHFILE")
  fi
  buildah --storage-driver vfs "${AUTHFILE_FLAGS[@]}" pull $BASE_IMAGE
  echo
  echo "Building image: $TARGET_IMAGE"
  buildah --storage-driver vfs bud -f /Dockerfile -t $TARGET_IMAGE .
  [[ $? -ne 0 ]] && { exit $BUILD_FAILED; }
  echo
}

function push() {
  echo "[Step: push]"
  buildah --storage-driver vfs --authfile $PUSH_SECRET_AUTHFILE push --format docker $TARGET_IMAGE
  [[ $? -ne 0 ]] && { exit $PUSH_FAILED; }
  echo
}

function completed() {
  echo "[Completed]"
  buildah --storage-driver vfs images
  echo
}

init
build
push
completed
