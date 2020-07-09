#!/usr/bin/env python3

# Usage: $0 IMAGE_URI FORCE_DEV_MODE
#
# Edits deploy/operator.yaml to do the following:
# - Replace the container image with IMAGE_URI. (We expect IMAGE_URI to come
#   from the $(OPERATOR_IMAGE_URI) calculated via `make`.)
# - Add env var FORCE_DEV_MODE with value FORCE_DEV_MODE. FORCE_DEV_MODE may
#   be "local", "cluster", or "" (empty -- production mode), but is mandatory.
#
# Dumps the updated operator.yaml to stdout

# NOTE(efried): We expect this script to be short-lived and replaced with some
# kind of proper templating, so little or no care is taken to make it robust
# or friendly.

import sys
import yaml

# Hardcoded. Assumes this is run from the repo root. Assumes the file hasn't
# been moved/renamed.
OP_YAML = "deploy/operator.yaml"

image_uri = sys.argv[1]
dev_mode = sys.argv[2]

with open(OP_YAML) as f:
    y = yaml.safe_load(f)

container = y["spec"]["template"]["spec"]["containers"][0]
# Replace the container image
container["image"] = image_uri
# Add the FORCE_DEV_MODE env var
container["env"].append(dict(name="FORCE_DEV_MODE", value=dev_mode))

print(yaml.dump(y, default_flow_style=False))

sys.exit(0)
