#!/usr/bin/env bash
# Copyright (C) 2026 s12ryt
# SPDX-License-Identifier: GPL-3.0-only

set -u

readonly S12RYT_IPV6_RELEASE_API_DEFAULT="https://api.github.com/repos/s12ryt/s12ryt-ipv6/releases/latest"
readonly S12RYT_IPV6_RAW_BASE_DEFAULT="https://raw.githubusercontent.com/s12ryt/s12ryt-ipv6"

map_ipv6_project_arch() {
    case "$1" in
        x86_64 | amd64)
            printf 'amd64\n'
            ;;
        aarch64 | arm64)
            printf 'arm64\n'
            ;;
        *)
            printf '錯誤：不支援的架構：%s。\n' "$1" >&2
            return 1
            ;;
    esac
}

detect_ipv6_project_init() {
    if [[ -n "${S12RYT_INIT_SYSTEM:-}" ]]; then
        printf '%s\n' "$S12RYT_INIT_SYSTEM"
        return 0
    fi
    if [[ -d /run/systemd/system ]] && command -v systemctl >/dev/null 2>&1; then
        printf 'systemd\n'
        return 0
    fi
    printf 'unknown\n'
}

check_ipv6_project_distribution() {
    local os_release_file distribution version
    local ID='' VERSION_ID=''

    os_release_file="${S12RYT_OS_RELEASE_FILE:-/etc/os-release}"
    if [[ ! -r "$os_release_file" ]]; then
        printf '錯誤：無法讀取作業系統版本：%s。\n' "$os_release_file" >&2
        return 1
    fi

    # shellcheck disable=SC1090
    . "$os_release_file"
    distribution="${ID,,}"
    version="${VERSION_ID:-}"
    case "${distribution}:${version}" in
        debian:12 | debian:13 | ubuntu:24.04)
            return 0
            ;;
        *)
            printf '錯誤：多 IPv6 出站僅支援 Debian 12/13 或 Ubuntu 24.04。\n' >&2
            return 1
            ;;
    esac
}

check_ipv6_project_preflight() {
    local kernel_name effective_uid init_system machine_arch

    kernel_name="${S12RYT_KERNEL_NAME:-$(uname -s)}"
    if [[ "$kernel_name" != 'Linux' ]]; then
        printf '錯誤：多 IPv6 出站僅支援 Linux。\n' >&2
        return 1
    fi

    effective_uid="${S12RYT_EFFECTIVE_UID:-$EUID}"
    if [[ ! "$effective_uid" =~ ^[0-9]+$ ]] || ((effective_uid != 0)); then
        printf '錯誤：多 IPv6 出站需要 root 權限。\n' >&2
        return 1
    fi

    init_system="$(detect_ipv6_project_init)"
    if [[ "$init_system" != 'systemd' ]]; then
        printf '錯誤：多 IPv6 出站僅支援 systemd。\n' >&2
        return 1
    fi

    machine_arch="${S12RYT_MACHINE_ARCH:-$(uname -m)}"
    map_ipv6_project_arch "$machine_arch" >/dev/null || return 1
    check_ipv6_project_distribution
}

extract_ipv6_release_tag() {
    local metadata_file="$1"
    local release_tag=''

    if command -v jq >/dev/null 2>&1; then
        release_tag="$(jq -er '
            if type == "object" and .draft == false and .prerelease == false and
                (.tag_name | type == "string")
            then .tag_name
            else empty
            end
        ' "$metadata_file" 2>/dev/null)" || release_tag=''
    elif command -v python3 >/dev/null 2>&1; then
        release_tag="$(python3 - "$metadata_file" <<'PY' 2>/dev/null
import json
import sys

with open(sys.argv[1], encoding="utf-8") as source:
    release = json.load(source)
if not isinstance(release, dict):
    raise ValueError("release metadata must be an object")
if release.get("draft") is not False or release.get("prerelease") is not False:
    raise ValueError("release is not stable")
tag = release.get("tag_name")
if not isinstance(tag, str):
    raise ValueError("tag_name must be a string")
print(tag)
PY
        )" || release_tag=''
    else
        printf '錯誤：解析 GitHub Release metadata 需要 jq 或 python3。\n' >&2
        return 1
    fi

    if [[ ! "$release_tag" =~ ^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$ ]]; then
        printf '錯誤：GitHub Release metadata 無效。\n' >&2
        return 1
    fi
    printf '%s\n' "$release_tag"
}

legacy_ipv6_installation_exists() {
    local legacy_root="$1"
    local unit_path="$2"
    local network_unit_path="$3"

    if [[ -e "$network_unit_path" ]]; then
        return 0
    fi
    if [[ -f "$unit_path" ]] && grep -Fq -- "$legacy_root" "$unit_path"; then
        return 0
    fi
    if [[ ! -e "$unit_path" && -x "${legacy_root}/bin/s12ryt-ipv6" &&
        -e "${legacy_root}/state/integration.json" ]]; then
        return 0
    fi
    return 1
}

service_was_enabled() {
    systemctl is-enabled "$1" >/dev/null 2>&1
}

service_was_active() {
    systemctl is-active "$1" >/dev/null 2>&1
}

restore_legacy_ipv6_services() {
    local main_enabled="$1"
    local main_active="$2"
    local network_enabled="$3"
    local network_active="$4"

    if [[ "$main_enabled" == '1' ]]; then
        systemctl enable s12ryt-ipv6.service >/dev/null 2>&1 || true
    fi
    if [[ "$main_active" == '1' ]]; then
        systemctl start s12ryt-ipv6.service >/dev/null 2>&1 || true
    fi
    if [[ "$network_enabled" == '1' ]]; then
        systemctl enable s12ryt-ipv6-network.service >/dev/null 2>&1 || true
    fi
    if [[ "$network_active" == '1' ]]; then
        systemctl start s12ryt-ipv6-network.service >/dev/null 2>&1 || true
    fi
}

restore_legacy_ipv6_units() {
    local backup_directory="$1"
    local unit_path="$2"
    local network_unit_path="$3"

    [[ ! -e "${backup_directory}/main.service" ]] || \
        cp -p -- "${backup_directory}/main.service" "$unit_path" >/dev/null 2>&1 || true
    [[ ! -e "${backup_directory}/network.service" ]] || \
        cp -p -- "${backup_directory}/network.service" "$network_unit_path" >/dev/null 2>&1 || true
    systemctl daemon-reload >/dev/null 2>&1 || true
}

migrate_legacy_ipv6_installation() {
    local legacy_root unit_path network_unit_path legacy_binary
    local main_enabled=0 main_active=0 network_enabled=0 network_active=0
    local backup_directory=''

    legacy_root="${S12RYT_LEGACY_ROOT:-/opt/s12ryt-ipv6}"
    unit_path="${S12RYT_SYSTEMD_UNIT_PATH:-/etc/systemd/system/s12ryt-ipv6.service}"
    network_unit_path="${S12RYT_SYSTEMD_NETWORK_UNIT_PATH:-/etc/systemd/system/s12ryt-ipv6-network.service}"
    legacy_binary="${legacy_root}/bin/s12ryt-ipv6"

    if ! legacy_ipv6_installation_exists "$legacy_root" "$unit_path" "$network_unit_path"; then
        return 0
    fi
    if [[ ! -x "$legacy_binary" ]]; then
        printf '錯誤：偵測到舊版部署，但缺少可執行的舊版清理工具；已停止遷移。\n' >&2
        return 1
    fi

    [[ ! -e "$unit_path" ]] || ! service_was_enabled s12ryt-ipv6.service || main_enabled=1
    [[ ! -e "$unit_path" ]] || ! service_was_active s12ryt-ipv6.service || main_active=1
    [[ ! -e "$network_unit_path" ]] || ! service_was_enabled s12ryt-ipv6-network.service || network_enabled=1
    [[ ! -e "$network_unit_path" ]] || ! service_was_active s12ryt-ipv6-network.service || network_active=1

    if ! "$legacy_binary" cleanup-system; then
        printf '錯誤：舊版系統整合狀態清理失敗；已停止安裝新版。\n' >&2
        return 1
    fi

    if [[ -e "$unit_path" ]] && ! systemctl disable --now s12ryt-ipv6.service; then
        restore_legacy_ipv6_services "$main_enabled" "$main_active" "$network_enabled" "$network_active"
        printf '錯誤：舊版服務移除失敗；已盡力恢復原啟用與運行狀態。\n' >&2
        return 1
    fi
    if [[ -e "$network_unit_path" ]] && ! systemctl disable --now s12ryt-ipv6-network.service; then
        restore_legacy_ipv6_services "$main_enabled" "$main_active" "$network_enabled" "$network_active"
        printf '錯誤：舊版服務移除失敗；已盡力恢復原啟用與運行狀態。\n' >&2
        return 1
    fi

    backup_directory="$(mktemp -d "${TMPDIR:-/tmp}/s12ryt-ipv6-legacy-units.XXXXXX")" || {
        restore_legacy_ipv6_services "$main_enabled" "$main_active" "$network_enabled" "$network_active"
        printf '錯誤：無法建立舊版服務備份；已停止遷移。\n' >&2
        return 1
    }
    if [[ -e "$unit_path" ]] && ! cp -p -- "$unit_path" "${backup_directory}/main.service"; then
        restore_legacy_ipv6_services "$main_enabled" "$main_active" "$network_enabled" "$network_active"
        rm -rf -- "$backup_directory"
        printf '錯誤：舊版服務備份失敗；已盡力恢復原啟用與運行狀態。\n' >&2
        return 1
    fi
    if [[ -e "$network_unit_path" ]] && \
        ! cp -p -- "$network_unit_path" "${backup_directory}/network.service"; then
        restore_legacy_ipv6_services "$main_enabled" "$main_active" "$network_enabled" "$network_active"
        rm -rf -- "$backup_directory"
        printf '錯誤：舊版服務備份失敗；已盡力恢復原啟用與運行狀態。\n' >&2
        return 1
    fi
    if ! rm -f -- "$unit_path" "$network_unit_path"; then
        restore_legacy_ipv6_units "$backup_directory" "$unit_path" "$network_unit_path"
        restore_legacy_ipv6_services "$main_enabled" "$main_active" "$network_enabled" "$network_active"
        rm -rf -- "$backup_directory"
        printf '錯誤：舊版服務移除失敗；已盡力恢復原啟用與運行狀態。\n' >&2
        return 1
    fi

    if ! systemctl daemon-reload; then
        restore_legacy_ipv6_units "$backup_directory" "$unit_path" "$network_unit_path"
        restore_legacy_ipv6_services "$main_enabled" "$main_active" "$network_enabled" "$network_active"
        rm -rf -- "$backup_directory"
        printf '錯誤：舊版服務移除失敗；已盡力恢復原啟用與運行狀態。\n' >&2
        return 1
    fi

    rm -rf -- "$backup_directory"
    printf '舊版服務已移除；%s 內的資料已完整保留。\n' "$legacy_root"
}

download_and_run_upstream_ipv6_script() {
    local action="$1"
    local release_api raw_base script_path temporary_directory metadata_file script_file
    local release_tag script_url script_status=0

    release_api="${S12RYT_RELEASE_API_URL:-$S12RYT_IPV6_RELEASE_API_DEFAULT}"
    raw_base="${S12RYT_RAW_BASE_URL:-$S12RYT_IPV6_RAW_BASE_DEFAULT}"
    case "$action" in
        install | update)
            script_path='install.sh'
            ;;
        uninstall)
            script_path='deploy/uninstall.sh'
            ;;
        *)
            printf '錯誤：不支援的多 IPv6 操作：%s。\n' "$action" >&2
            return 1
            ;;
    esac

    if ! command -v curl >/dev/null 2>&1; then
        printf '錯誤：下載上游腳本需要 curl。\n' >&2
        return 1
    fi
    temporary_directory="$(mktemp -d "${TMPDIR:-/tmp}/s12ryt-ipv6-upstream.XXXXXX")" || {
        printf '錯誤：無法建立上游腳本暫存目錄。\n' >&2
        return 1
    }
    metadata_file="${temporary_directory}/release.json"
    script_file="${temporary_directory}/upstream.sh"

    if ! curl -fsSL --connect-timeout 5 --max-time 60 "$release_api" -o "$metadata_file"; then
        rm -rf -- "$temporary_directory"
        printf '錯誤：無法查詢 s12ryt-ipv6 最新正式 Release。\n' >&2
        return 1
    fi
    release_tag="$(extract_ipv6_release_tag "$metadata_file")" || {
        rm -rf -- "$temporary_directory"
        return 1
    }
    script_url="${raw_base}/${release_tag}/${script_path}"
    if ! curl -fsSL --connect-timeout 5 --max-time 60 "$script_url" -o "$script_file"; then
        rm -rf -- "$temporary_directory"
        printf '錯誤：無法下載 s12ryt-ipv6 上游腳本。\n' >&2
        return 1
    fi
    if ! bash -n "$script_file"; then
        rm -rf -- "$temporary_directory"
        printf '錯誤：上游腳本語法驗證失敗，未執行。\n' >&2
        return 1
    fi

    VERSION="$release_tag" bash "$script_file" || script_status=$?
    rm -rf -- "$temporary_directory"
    if ((script_status != 0)); then
        printf '錯誤：s12ryt-ipv6 上游腳本執行失敗。\n' >&2
        return "$script_status"
    fi
}

run_ipv6_project_action() {
    local action="$1"

    check_ipv6_project_preflight || return 1
    if [[ "$action" == 'install' || "$action" == 'update' ]]; then
        migrate_legacy_ipv6_installation || return 1
    fi
    download_and_run_upstream_ipv6_script "$action"
}

main() {
    if (($# != 1)); then
        printf '用法：%s [preflight|install|update|uninstall]\n' "${0##*/}" >&2
        return 1
    fi
    case "$1" in
        preflight)
            check_ipv6_project_preflight
            ;;
        install | update | uninstall)
            run_ipv6_project_action "$1"
            ;;
        *)
            printf '用法：%s [preflight|install|update|uninstall]\n' "${0##*/}" >&2
            return 1
            ;;
    esac
}

if [[ "${BASH_SOURCE[0]}" == "$0" && "${S12RYT_IPV6_SOURCE_ONLY:-0}" != '1' ]]; then
    main "$@"
fi
