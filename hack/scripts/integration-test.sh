#!/bin/bash

echo "Test Integration WIP"
export FAKE_KEY_VALUE=$(cat /tmp/secret/aao-aws-creds/FAKE_KEY_VALUE)
echo $FAKE_KEY_VALUE
echo $NAMESPACE
