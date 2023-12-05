#!/usr/bin/env bash
set -euo pipefail

SERVICE='ns1_exporter'

# create ns1_exporter system user
if ! getent passwd ${SERVICE} > /dev/null 2>&1; then
    useradd --system --skel '/dev/null' --create-home --home-dir "/var/lib/${SERVICE}" --shell '/bin/false' --user-group "${SERVICE}"
fi
