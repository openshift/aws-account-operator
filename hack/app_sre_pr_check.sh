#!/bin/bash

# AppSRE team CD

set -exv

curl -s https://raw.githubusercontent.com/maorfr/utilities/master/validate_yaml.py > validate_yaml.py
python validate_yaml.py deploy/crds
if [ "$?" != "0" ]; then
    exit 1
fi
rm validate_yaml.py

BASE_IMG="aws-account-operator"
IMG="${BASE_IMG}:latest"

BUILD_CMD="docker build" IMG="$IMG" make docker-build
