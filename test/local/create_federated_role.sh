#!/bin/bash

source test/integration/test_envs

oc apply -f deploy_pko/AWSFederatedRole-network-mgmt.yaml -f deploy_pko/AWSFederatedRole-read-only.yaml
