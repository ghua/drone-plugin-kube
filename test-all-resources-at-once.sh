#!/bin/bash

export PLUGIN_TEMPLATE=test/all-resources-at-once.yaml
export PLUGIN_NAME=drone-kube-test
export PLUGIN_NAMESPACE=default
export PLUGIN_CONFIGMAP_FILE=test/sample-config-data

go build -o build/kubano
export $(cat .env | xargs) && ./build/kubano

# docker run --env-file=.env drone-kubano
