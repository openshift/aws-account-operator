#!/bin/bash

# Script to update AWS credentials for osd-staging-1 and osd-staging-2
# using rh-aws-saml-login

set -e

CREDENTIALS_FILE="$HOME/.aws/credentials"
PROFILES=("osd-staging-1" "osd-staging-2")

# Function to update credentials for a profile
update_profile() {
    local profile=$1

    echo "Logging in to $profile..."

    # Run rh-aws-saml-login and capture the environment
    # The command should set AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY, and AWS_SESSION_TOKEN
    eval $(rh-aws-saml-login "$profile" --output env 2>/dev/null)

    # Check if credentials were set
    if [[ -z "$AWS_ACCESS_KEY_ID" ]] || [[ -z "$AWS_SECRET_ACCESS_KEY" ]] || [[ -z "$AWS_SESSION_TOKEN" ]]; then
        echo "Error: Failed to get credentials for $profile"
        return 1
    fi

    echo "Updating credentials file for [$profile]..."

    # Create a temporary file with updated credentials
    python3 - "$CREDENTIALS_FILE" "$profile" "$AWS_ACCESS_KEY_ID" "$AWS_SECRET_ACCESS_KEY" "$AWS_SESSION_TOKEN" << 'PYTHON_EOF'
import sys
import re

creds_file = sys.argv[1]
profile_name = sys.argv[2]
access_key = sys.argv[3]
secret_key = sys.argv[4]
session_token = sys.argv[5]

# Read the credentials file
with open(creds_file, 'r') as f:
    lines = f.readlines()

# Find and update the profile section
profile_section = f'[{profile_name}]'
in_profile = False
profile_found = False
new_lines = []
i = 0

while i < len(lines):
    line = lines[i].rstrip('\n')

    # Check if this is the target profile
    if line == profile_section:
        profile_found = True
        new_lines.append(line)
        new_lines.append(f'aws_access_key_id = {access_key}')
        new_lines.append(f'aws_secret_access_key = {secret_key}')
        new_lines.append(f'aws_session_token = {session_token}')
        i += 1

        # Skip old credentials and empty lines in this section
        while i < len(lines):
            next_line = lines[i].rstrip('\n')
            # Stop at next section
            if next_line.startswith('['):
                new_lines.append('')
                break
            # Skip aws credential lines
            if next_line.startswith('aws_access_key_id') or \
               next_line.startswith('aws_secret_access_key') or \
               next_line.startswith('aws_session_token') or \
               next_line.strip() == '':
                i += 1
                continue
            # Keep other lines
            new_lines.append(next_line)
            i += 1
        continue

    new_lines.append(line)
    i += 1

# If profile wasn't found, add it at the end
if not profile_found:
    if new_lines and new_lines[-1] != '':
        new_lines.append('')
    new_lines.append(profile_section)
    new_lines.append(f'aws_access_key_id = {access_key}')
    new_lines.append(f'aws_secret_access_key = {secret_key}')
    new_lines.append(f'aws_session_token = {session_token}')

# Write back to the file
with open(creds_file, 'w') as f:
    f.write('\n'.join(new_lines) + '\n')

PYTHON_EOF

    echo "✓ Updated $profile successfully"
    echo ""

    # Clear the environment variables for the next iteration
    unset AWS_ACCESS_KEY_ID AWS_SECRET_ACCESS_KEY AWS_SESSION_TOKEN
}

# TODO remove from final version
# Backup credentials file
cp "$CREDENTIALS_FILE" "$CREDENTIALS_FILE.backup"
echo "Backed up credentials file to $CREDENTIALS_FILE.backup"
echo ""

# Update each profile
for profile in "${PROFILES[@]}"; do
    update_profile "$profile" || echo "Warning: Failed to update $profile"
done

echo "Done! AWS credentials updated for all profiles."
