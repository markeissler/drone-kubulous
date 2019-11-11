#!/bin/bash

export PLUGIN_TEMPLATE=test/service.template.yaml
export PLUGIN_NAME=drone-kube-test

go build -o build/kubano
export $(cat .env | xargs) && ./build/kubano
