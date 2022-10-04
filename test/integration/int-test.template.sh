#!/bin/bash

# A template for individual integration tests executed by the integration test framework (integration-test-bootstrap.sh).
# To use:
# 1. Copy this file to a new file in in the tests directrory
# 2. Implement the beforeTestPhase, afterTestPhase, testPhase and explainExitCode functions stubbed out below
# 3. Update integration-test-bootstrap.sh to call the new test

# Load Environment vars
# Some int testing constants such as EXIT_RETRY/EXIT_PASS are defined in this file
# as well as constants used by various tests such as namespaces or account numbers
source hack/scripts/test_envs


# TODO: define custom exit codes for different failure scenarios 
# and user friendly messages to associate with them
$EXIT_TEST_FAILED_VALIDATION=2
declare -A exitCodeMessages
exitCodeMessages[$EXIT_TEST_FAILED_VALIDATION]="Test failed validation"

#
# The before test phase is used to do setup needed by the test before it runs.
# For example creating CRs which the operator you are testing acts on. This function may
# be called multiple times by the test framework so it should be idempotent.
#
# If this step was successful, or encounters failures that dont matter, your implementation
# should return a $EXIT_PASS (0) exit code so the next phase will be executed. 
# 
# If you need to wait on some condition to be met before proceeding, return $EXIT_RETRY (1) and 
# the test framework will wait for some interval before calling this function again (eventually a 
# timeout will occur and the test will be marked as failed). 
# 
# If the step failed in a unrecoverable way and the test should not proceed, return any other exit code.
#
# input: 
#   None
# return: 
#   $EXIT_PASS          - the function succeeded and the test should proceed to the next phase (test/testPhase)
#   $EXIT_RETRY         - the function is waiting for some condition to be met before proceeding
#   any other exit code - the function failed and the test should not proceed 
#       (note, the cleanup/afterTestPhase phase will still be executed)
function beforeTestPhase {

    #check some condition such as oc get account ...
    # integration-test-bootstrap.sh will recall this function until it stops 
    # returning $EXIT_RETRY, an error occurs or a timeout is met
    WAITING_FOR_CONDITION=0 
    if [ $WAITING_FOR_CONDITION -eq 1 ]; then
        echo "Waiting for condition to be met before proceeding"
        exit $EXIT_RETRY
    fi

    exit $EXIT_PASS
}

#
# The after test phase is used to do cleanup after the test has run. For example 
# deleting CRs created by beforeTestPhase. This function may be called multiple 
# times by the test framework so it should be idempotent. It will also always be called 
# whether the previous phases were successful or not.
#
# If this step was successful, or encounters failures that dont matter, your implementation
# should return a $EXIT_PASS (0) exit code so the next phase will be executed. 
# 
# If you need to wait on some condition to be met before proceeding, return $EXIT_RETRY (1) and 
# the test framework will wait for some interval before calling this function again (eventually a 
# timeout will occur and the test will be marked as failed). 
# 
# If the step failed in a unrecoverable way and the test should not proceed, return any other exit code.
#
# input: 
#   None
# return: 
#   $EXIT_PASS          - the function succeeded and the test should proceed to the next phase (none)
#   $EXIT_RETRY         - the function is waiting for some condition to be met before proceeding
#   any other exit code - the function failed and the test should not proceed
function afterTestPhase {

    #check some condition such as oc get account ...
    # integration-test-bootstrap.sh will recall this function until it stops 
    # returning $EXIT_RETRY, an error occurs or a timeout is met
    WAITING_FOR_CONDITION=0 
    if [ $WAITING_FOR_CONDITION -eq 1 ]; then
        echo "Waiting for condition to be met before proceeding"
        exit $EXIT_RETRY
    fi

    exit $EXIT_PASS
}

#
# The test phase is used to actually exercise and validate the functionality being tested.
# Since operators are automatically reacting to CRs (presumably in the beforeTestPhase)
# this function should generally be dedicated to validating the expected outcome after the operator
# has done its work. This function may be called multiple times by the test framework so 
# it should be idempotent.
#
# If this step was successful, or encounters failures that dont matter, your implementation
# should return a $EXIT_PASS (0) exit code so the next phase will be executed. 
# 
# If you need to wait on some condition to be met before proceeding, return $EXIT_RETRY (1) and 
# the test framework will wait for some interval before calling this function again (eventually a 
# timeout will occur and the test will be marked as failed). 
# 
# If the step failed in a unrecoverable way and the test should not proceed, return any other exit code.
#
# input: 
#   None
# return: 
#   $EXIT_PASS          - the function succeeded and the test should proceed to the next phase (test/testPhase)
#   $EXIT_RETRY         - the function is waiting for some condition to be met before proceeding
#   any other exit code - the function failed and the test should not proceed 
#       (note, the cleanup/afterTestPhase phase will still be executed)
function testPhase {
    
    #check some condition such as oc get account ...
    # integration-test-bootstrap.sh will recall this function until it stops 
    # returning $EXIT_RETRY, an error occurs or a timeout is met
    WAITING_FOR_CONDITION=0 
    if [ $WAITING_FOR_CONDITION -eq 1 ]; then
        echo "Waiting for condition to be met before proceeding"
        exit $EXIT_RETRY
    fi

    exit $EXIT_PASS
}

# This is a convenience method for getting a human friendly message for an exit code.
# It should not perform any long running tasks and should only print a message to stdout 
# based on the provided exit code.
#
# input:
#   $1 - the execution phase the exit code was encountered in (not guaranteed to be a valid phase)
#   $2 - the exit code to get a message for (not guaranteed to be a known/sane exit code)
# return (side effect):
#   stdout - the exit code message
function explainExitCode {
    local phase=$1
    local exitCode=$2
    local message=${exitCodeMessages[$exitCode]}
    echo $message
}

# The phase are specific keys passed in by the test framework. You can change function names if you want
# but do not change the phase names used as keys in the switch statement.
PHASE=$1
case $PHASE in
    setup)
        setupTestPhase
        ;;
    cleanup)
        cleanupTestPhase
        ;;
    test)
        testPhase
        ;;
    explain)
        explainExitCode $PHASE $2
        ;;
    *)
        echo "Unknown test phase: '$PHASE'"
        exit 1
        ;;
esac