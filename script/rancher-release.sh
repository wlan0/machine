#!/bin/bash

set -e

BASEDIR=$(dirname $0)/..

cd $BASEDIR

ABSPATH=$(pwd)

GOOS=linux GOARCH=amd64 make build

mkdir -p $ABSPATH/dist/artifacts

cd $ABSPATH/bin

tar -cvzf $ABSPATH/dist/artifacts/docker-machine.tar.gz *

