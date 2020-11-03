#!/bin/bash

rm -rf build/kubano
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o build/kubano

docker build -t markeissler/drone-kubulous:0.2.0 -t markeissler/drone-kubulous:latest build
docker push markeissler/drone-kubulous:0.2.0
docker push markeissler/drone-kubulous:latest

