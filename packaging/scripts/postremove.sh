#!/usr/bin/env bash
set -euo pipefail

SERVICE='ns1_exporter'

# delete user/group
userdel --force --remove "${SERVICE}"
groupdel "${SERVICE}" || true # ensure group is deleted if `USERGROUPS_ENAB` is disabled in `/etc/login.defs`

# remove config dir
rm -rf "/etc/${SERVICE}"
