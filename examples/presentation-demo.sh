#!/usr/bin/env bash
set -euo pipefail
./bin/agentguard check testdata/node_modules_fixture --ecosystem node --no-color --exit-on-finding=false
./bin/agentguard check testdata/go_fixture --ecosystem go --no-color --exit-on-finding=false
./bin/agentguard corpus
