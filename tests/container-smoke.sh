#!/usr/bin/env bash
# Copyright (C) 2026 s12ryt
# SPDX-License-Identifier: GPL-3.0-only

set -euo pipefail

readonly EXPECTED_MANAGER="${1:?需要預期的套件管理器名稱}"

cd /workspace
bash -n s12ryt.sh install-proot.sh

export S12RYT_SOURCE_ONLY=1
# shellcheck source=s12ryt.sh
source ./s12ryt.sh
actual_manager="$(detect_package_manager)"
if [[ "$actual_manager" != "$EXPECTED_MANAGER" ]]; then
    printf '預期套件管理器 %s，實際為 %s\n' "$EXPECTED_MANAGER" "$actual_manager" >&2
    exit 1
fi

export S12RYT_PROOT_SOURCE_ONLY=1
# shellcheck source=install-proot.sh
source ./install-proot.sh
if [[ "$(map_host_arch "$(uname -m)")" != "linux/amd64" ]]; then
    printf '容器並非預期的 x86_64/amd64 平台。\n' >&2
    exit 1
fi
