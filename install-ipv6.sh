#!/usr/bin/env bash
# Copyright (C) 2026 s12ryt
# SPDX-License-Identifier: GPL-3.0-only

set -u

readonly IPV6_PROJECT_VERSION="1.1.0"
readonly SINGBOX_VERSION="1.13.15"
readonly SINGBOX_AMD64_SHA256="a3a3ff223b23c3f4731d0a17cb0ef94c97ce257c70721a5b07dc7ca079203c9f"
readonly SINGBOX_ARM64_SHA256="f0810bbb5722ae36635687c421019defcc8b328d31a0b3c287901f331747ca93"

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
    if command -v rc-service >/dev/null 2>&1; then
        printf 'openrc\n'
        return 0
    fi

    printf 'unknown\n'
}

check_ipv6_project_preflight() {
    local kernel_name effective_uid init_system machine_arch

    kernel_name="${S12RYT_KERNEL_NAME:-$(uname -s)}"
    if [[ "$kernel_name" != "Linux" ]]; then
        printf '錯誤：多 IPv6 出站僅支援 Linux。\n' >&2
        return 1
    fi

    effective_uid="${S12RYT_EFFECTIVE_UID:-$EUID}"
    if [[ ! "$effective_uid" =~ ^[0-9]+$ ]] || ((effective_uid != 0)); then
        printf '錯誤：多 IPv6 出站需要 root 權限。\n' >&2
        return 1
    fi

    init_system="$(detect_ipv6_project_init)"
    if [[ "$init_system" != "systemd" && "$init_system" != "openrc" ]]; then
        printf '錯誤：多 IPv6 出站僅支援 systemd 或 OpenRC。\n' >&2
        return 1
    fi

    machine_arch="${S12RYT_MACHINE_ARCH:-$(uname -m)}"
    if ! map_ipv6_project_arch "$machine_arch" >/dev/null; then
        return 1
    fi

    return 0
}

panel_asset_name() {
    case "$1" in
        amd64 | arm64)
            printf 's12ryt-ipv6-linux-%s\n' "$1"
            ;;
        *)
            printf '錯誤：不支援的面板資產架構：%s。\n' "$1" >&2
            return 1
            ;;
    esac
}

singbox_asset_name() {
    case "$1" in
        amd64 | arm64)
            printf 'sing-box-%s-linux-%s.tar.gz\n' "$SINGBOX_VERSION" "$1"
            ;;
        *)
            printf '錯誤：不支援的 sing-box 資產架構：%s。\n' "$1" >&2
            return 1
            ;;
    esac
}

singbox_asset_digest() {
    case "$1" in
        amd64)
            printf '%s\n' "$SINGBOX_AMD64_SHA256"
            ;;
        arm64)
            printf '%s\n' "$SINGBOX_ARM64_SHA256"
            ;;
        *)
            printf '錯誤：不支援的 sing-box digest 架構：%s。\n' "$1" >&2
            return 1
            ;;
    esac
}

sha256_of_file() {
    local output

    output="$(sha256sum "$1")" || return 1
    printf '%s\n' "${output%% *}"
}

checksum_for_asset() {
    local checksum_file="$1"
    local asset_name="$2"

    awk -v asset="$asset_name" '
        $2 == asset { digest = $1; matches++ }
        END {
            if (matches != 1) exit 1
            print digest
        }
    ' "$checksum_file"
}

valid_sha256() {
    [[ "$1" =~ ^[0-9a-fA-F]{64}$ ]]
}

cleanup_ipv6_download() {
    local temporary_directory="$1"

    if [[ -n "$temporary_directory" && -d "$temporary_directory" ]]; then
        rm -rf -- "$temporary_directory"
    fi
}

fetch_ipv6_release_bundle() {
    local destination="$1"
    local architecture="$2"
    local parent temporary_directory panel_asset singbox_asset
    local panel_url checksums_url singbox_url panel_expected panel_actual
    local singbox_expected singbox_actual archive_entry

    panel_asset="$(panel_asset_name "$architecture")" || return 1
    singbox_asset="$(singbox_asset_name "$architecture")" || return 1

    for command_name in curl sha256sum awk tar mktemp mkdir mv rm; do
        if ! command -v "$command_name" >/dev/null 2>&1; then
            printf '錯誤：缺少必要命令：%s。\n' "$command_name" >&2
            return 1
        fi
    done

    parent="${destination%/*}"
    if [[ "$parent" == "$destination" ]]; then
        parent='.'
    fi
    if ! mkdir -p -- "$parent"; then
        printf '錯誤：無法建立 IPv6 專案下載目錄。\n' >&2
        return 1
    fi
    temporary_directory="$(mktemp -d "${parent}/.s12ryt-ipv6-download.XXXXXX")" || {
        printf '錯誤：無法建立 IPv6 專案下載暫存目錄。\n' >&2
        return 1
    }

    panel_url="https://github.com/s12ryt/s12ryt-vps-sh/releases/download/v${IPV6_PROJECT_VERSION}/${panel_asset}"
    checksums_url="https://github.com/s12ryt/s12ryt-vps-sh/releases/download/v${IPV6_PROJECT_VERSION}/SHA256SUMS"
    singbox_url="https://github.com/SagerNet/sing-box/releases/download/v${SINGBOX_VERSION}/${singbox_asset}"

    if ! curl -fsSL --connect-timeout 5 --max-time 60 "$panel_url" -o "${temporary_directory}/${panel_asset}" ||
        ! curl -fsSL --connect-timeout 5 --max-time 60 "$checksums_url" -o "${temporary_directory}/SHA256SUMS" ||
        ! curl -fsSL --connect-timeout 5 --max-time 60 "$singbox_url" -o "${temporary_directory}/${singbox_asset}"; then
        cleanup_ipv6_download "$temporary_directory"
        printf '錯誤：無法下載 IPv6 專案資產。\n' >&2
        return 1
    fi

    panel_expected="$(checksum_for_asset "${temporary_directory}/SHA256SUMS" "$panel_asset")" || panel_expected=''
    panel_actual="$(sha256_of_file "${temporary_directory}/${panel_asset}")" || panel_actual=''
    if ! valid_sha256 "$panel_expected" || [[ "${panel_expected,,}" != "${panel_actual,,}" ]]; then
        cleanup_ipv6_download "$temporary_directory"
        printf '錯誤：面板資產 SHA256 驗證失敗。\n' >&2
        return 1
    fi

    singbox_expected="${S12RYT_SINGBOX_SHA256:-$(singbox_asset_digest "$architecture")}" || singbox_expected=''
    singbox_actual="$(sha256_of_file "${temporary_directory}/${singbox_asset}")" || singbox_actual=''
    if ! valid_sha256 "$singbox_expected" || [[ "${singbox_expected,,}" != "${singbox_actual,,}" ]]; then
        cleanup_ipv6_download "$temporary_directory"
        printf '錯誤：sing-box 資產 SHA256 驗證失敗。\n' >&2
        return 1
    fi

    archive_entry="sing-box-${SINGBOX_VERSION}-linux-${architecture}/sing-box"
    if ! tar -tzf "${temporary_directory}/${singbox_asset}" 2>/dev/null | grep -Fxq "$archive_entry"; then
        cleanup_ipv6_download "$temporary_directory"
        printf '錯誤：sing-box 壓縮檔驗證失敗。\n' >&2
        return 1
    fi

    if [[ -e "$destination" ]]; then
        cleanup_ipv6_download "$temporary_directory"
        printf '錯誤：IPv6 專案資產目標已存在。\n' >&2
        return 1
    fi
    if ! mv -- "$temporary_directory" "$destination"; then
        cleanup_ipv6_download "$temporary_directory"
        printf '錯誤：無法保存 IPv6 專案資產。\n' >&2
        return 1
    fi

    printf 'IPv6 專案資產下載與驗證完成。\n'
}

main() {
    case "${1:-}" in
        preflight | '')
            check_ipv6_project_preflight
            ;;
        fetch)
            if (($# != 3)); then
                printf '用法：%s fetch DESTINATION ARCH\n' "${0##*/}" >&2
                return 1
            fi
            fetch_ipv6_release_bundle "$2" "$3"
            ;;
        *)
            printf '用法：%s [preflight|fetch DESTINATION ARCH]\n' "${0##*/}" >&2
            return 1
            ;;
    esac
}

if [[ "${BASH_SOURCE[0]}" == "$0" && "${S12RYT_IPV6_SOURCE_ONLY:-0}" != "1" ]]; then
    main "$@"
fi
