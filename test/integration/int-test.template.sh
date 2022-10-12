#!/usr/bin/env bash

# A template for individual integration tests executed by the integration test framework (integration-test-bootstrap.sh).
# To use:
# 1. Copy this file to a new file in in the tests directory
# 2. Implement the setupTestPhase, afterTestPhase, testPhase and explainExitCode functions stubbed out below
# 3. Update integration-test-bootstrap.sh to call the new test
#
# Test Description:
#   TODO
#

# Test lib contains constants used for testing and helper functions
# such as createAccountCR or waitForAccountCRReadyOrFailed
source test/integration/integration-test-lib.sh


# TODO: define custom exit codes for different failure scenarios 
# and user friendly messages to associate with them
# EXIT_TEST_FAILED_VALIDATION=1
declare -A exitCodeMessages
EXIT_TEST_FAILED_EXAMPLE_EXIT_CODE=1
exitCodeMessages[$EXIT_TEST_FAILED_EXAMPLE_EXIT_CODE]="This is an example test failure message"

#
# The before test phase is used to do setup needed by the test before it runs.
# For example creating CRs which the operator you are testing acts on.
#
# If you need to wait for some condition to be met before proceeding it is up to 
# you to implement that polling logic, good examples of how to do that with 
# timeouts can be found in the integration-test-lib.sh file. 
#
# If this step was successful, or encounters failures that dont matter, your implementation
# should return a $EXIT_PASS (0) exit code so the next phase will be executed.If the step 
# failed in a unrecoverable way and the test should not proceed, return any other exit code.
#
# input: 
#   None
# return: 
#   $EXIT_PASS          - the function succeeded and the test should proceed to the next phase (test/testPhase)
#   any other exit code - the function failed and the test should not proceed 
#       (note, the cleanup/afterTestPhase phase will still be executed)
function setupTestPhase {
    exit "$EXIT_PASS"
}

#
# The after test phase is used to do cleanup after the test has run. For example 
# deleting CRs created by setupTestPhase. 
#
# If you need to wait for some condition to be met before proceeding it is up to 
# you to implement that polling logic, good examples of how to do that with 
# timeouts can be found in the integration-test-lib.sh file. 
#
# If this step was successful, or encounters failures that dont matter, your implementation
# should return a $EXIT_PASS (0) exit code so the next phase will be executed. If the step 
# failed in a unrecoverable way and the test should not proceed, return any other exit code.
#
# input: 
#   None
# return: 
#   $EXIT_PASS          - the function succeeded and the test should proceed to the next phase (none)
#   any other exit code - the function failed and the test should not proceed
function cleanupTestPhase {
    exit "$EXIT_PASS"
}

#
# The test phase is used to actually exercise and validate the functionality being tested.
# Since operators are automatically reacting to CRs (presumably in the setupTestPhase)
# this function should generally be dedicated to validating the expected outcome after the operator
# has done its work.
#
# If you need to wait for some condition to be met before proceeding it is up to 
# you to implement that polling logic, good examples of how to do that with 
# timeouts can be found in the integration-test-lib.sh file. 
#
# If this step was successful, or encounters failures that dont matter, your implementation
# should return a $EXIT_PASS (0) exit code so the next phase will be executed. If the step 
# failed in a unrecoverable way and the test should not proceed, return any other exit code.
#
# input: 
#   None
# return: 
#   $EXIT_PASS          - the function succeeded and the test should proceed to the next phase (test/testPhase)
#   any other exit code - the function failed and the test should not proceed 
#       (note, the cleanup/afterTestPhase phase will still be executed)
function testPhase {
    exit "$EXIT_PASS"
}

# This is a convenience method for getting a human friendly message for an exit code.
# It should not perform any long running tasks and should only print a message to stdout 
# based on the provided exit code.
#
# input:
#   $1 - the exit code to get a message for (not guaranteed to be a known/sane exit code)
# return (side effect):
#   stdout - the exit code message
function explainExitCode {
    local exitCode=$1
    local message=${exitCodeMessages[$exitCode]}
    echo "$message"
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
        explainExitCode "$2"
        ;;
    *)
        echo "Unknown test phase: '$PHASE'"
        exit 1
        ;;
esac