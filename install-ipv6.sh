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

write_ipv6_project_file() {
    local path="$1"
    local mode="$2"
    local content="$3"
    local parent temporary_file

    parent="${path%/*}"
    if [[ "$parent" == "$path" ]]; then
        parent='.'
    fi
    mkdir -p -- "$parent" || return 1
    temporary_file="$(mktemp "${parent}/.s12ryt-ipv6-file.XXXXXX")" || return 1
    if ! printf '%s' "$content" > "$temporary_file" ||
        ! chmod "$mode" "$temporary_file" ||
        ! mv -f -- "$temporary_file" "$path"; then
        rm -f -- "$temporary_file"
        return 1
    fi
}

systemd_unit_content() {
    local project_root="$1"

    cat <<EOF
[Unit]
Description=s12ryt IPv6 outbound panel
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=root
ExecStart=${project_root}/bin/s12ryt-ipv6 serve
Restart=on-failure
RestartSec=3
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
PrivateTmp=true
ReadWritePaths=${project_root}
CapabilityBoundingSet=CAP_NET_ADMIN CAP_NET_BIND_SERVICE

[Install]
WantedBy=multi-user.target
EOF
}

openrc_service_content() {
    local project_root="$1"

    cat <<EOF
#!/sbin/openrc-run
name="s12ryt IPv6 outbound panel"
description="s12ryt IPv6 outbound panel"
command="${project_root}/bin/s12ryt-ipv6"
command_args="serve"
command_background=true
pidfile=/run/s12ryt-ipv6.pid
output_log=/var/log/s12ryt-ipv6/panel.log
error_log=/var/log/s12ryt-ipv6/panel.log

depend() {
    need net
    after firewall
}
EOF
}

openrc_logrotate_content() {
    cat <<'EOF'
/var/log/s12ryt-ipv6/*.log {
    daily
    rotate 7
    size 100M
    missingok
    notifempty
    copytruncate
}
EOF
}

cleanup_new_ipv6_installation() {
    local project_root="$1"
    local remove_state="$2"

    rm -f -- "${project_root}/bin/s12ryt-ipv6" "${project_root}/bin/sing-box"
    if [[ "$remove_state" == "1" ]]; then
        rm -rf -- "${project_root}/config" "${project_root}/secrets"
    fi
}

install_verified_ipv6_bundle() {
    local bundle="$1"
    local architecture="$2"
    local init_system="$3"
    local project_root panel_asset singbox_asset archive_entry
    local binary_directory temporary_directory panel_source archive_source
    local panel_temporary singbox_temporary had_state=0
    local unit_path service_path logrotate_path command_name

    panel_asset="$(panel_asset_name "$architecture")" || return 1
    singbox_asset="$(singbox_asset_name "$architecture")" || return 1
    project_root="${S12RYT_PROJECT_ROOT:-/opt/s12ryt-ipv6}"
    binary_directory="${project_root}/bin"
    panel_source="${bundle}/${panel_asset}"
    archive_source="${bundle}/${singbox_asset}"
    archive_entry="sing-box-${SINGBOX_VERSION}-linux-${architecture}/sing-box"

    if [[ ! -f "$panel_source" || ! -f "$archive_source" ]]; then
        printf '錯誤：已驗證的 IPv6 專案資產不完整。\n' >&2
        return 1
    fi
    if [[ "$init_system" != "systemd" && "$init_system" != "openrc" ]]; then
        printf '錯誤：多 IPv6 出站僅支援 systemd 或 OpenRC。\n' >&2
        return 1
    fi
    for command_name in chmod cp mkdir mktemp mv rm tar; do
        if ! command -v "$command_name" >/dev/null 2>&1; then
            printf '錯誤：缺少必要命令：%s。\n' "$command_name" >&2
            return 1
        fi
    done
    if [[ -e "${binary_directory}/s12ryt-ipv6" || -e "${binary_directory}/sing-box" ]]; then
        printf '錯誤：IPv6 專案 binary 已存在；請使用更新流程。\n' >&2
        return 1
    fi
    if [[ -e "${project_root}/config/config.json" || -e "${project_root}/secrets/password.hash" ||
        -e "${project_root}/secrets/management.password" ]]; then
        had_state=1
    fi

    mkdir -p -- "$binary_directory" || {
        printf '錯誤：無法建立 IPv6 專案 binary 目錄。\n' >&2
        return 1
    }
    chmod 0755 "$project_root" "$binary_directory" || return 1
    temporary_directory="$(mktemp -d "${project_root}/.s12ryt-ipv6-install.XXXXXX")" || {
        printf '錯誤：無法建立 IPv6 專案安裝暫存目錄。\n' >&2
        return 1
    }
    panel_temporary="${temporary_directory}/s12ryt-ipv6"
    singbox_temporary="${temporary_directory}/sing-box"

    if ! cp -- "$panel_source" "$panel_temporary" ||
        ! tar -xzf "$archive_source" -C "$temporary_directory" "$archive_entry" 2>/dev/null ||
        ! mv -- "${temporary_directory}/${archive_entry}" "$singbox_temporary" ||
        ! chmod 0755 "$panel_temporary" "$singbox_temporary" ||
        ! mv -- "$panel_temporary" "${binary_directory}/s12ryt-ipv6" ||
        ! mv -- "$singbox_temporary" "${binary_directory}/sing-box"; then
        rm -rf -- "$temporary_directory"
        cleanup_new_ipv6_installation "$project_root" 0
        printf '錯誤：無法部署 IPv6 專案 binary。\n' >&2
        return 1
    fi
    rm -rf -- "$temporary_directory"

    if ((had_state == 0)); then
        if ! S12RYT_PROJECT_ROOT="$project_root" "${binary_directory}/s12ryt-ipv6" init; then
            cleanup_new_ipv6_installation "$project_root" 1
            printf '錯誤：面板初始化失敗。\n' >&2
            return 1
        fi
    fi
    if [[ ! -f "${project_root}/config/config.json" || ! -f "${project_root}/secrets/password.hash" ||
        ! -f "${project_root}/secrets/management.password" ]]; then
        cleanup_new_ipv6_installation "$project_root" "$((had_state == 0 ? 1 : 0))"
        printf '錯誤：面板初始化狀態不完整。\n' >&2
        return 1
    fi
    chmod 0700 "${project_root}/config" "${project_root}/secrets" || return 1
    chmod 0600 "${project_root}/config/config.json" "${project_root}/secrets/password.hash" \
        "${project_root}/secrets/management.password" || return 1

    if [[ "$init_system" == "systemd" ]]; then
        unit_path="${S12RYT_SYSTEMD_UNIT_PATH:-/etc/systemd/system/s12ryt-ipv6.service}"
        if ! write_ipv6_project_file "$unit_path" 0644 "$(systemd_unit_content "$project_root")" ||
            ! systemctl daemon-reload ||
            ! systemctl enable --now s12ryt-ipv6.service; then
            rm -f -- "$unit_path"
            systemctl daemon-reload >/dev/null 2>&1 || true
            cleanup_new_ipv6_installation "$project_root" "$((had_state == 0 ? 1 : 0))"
            printf '錯誤：無法註冊 systemd 服務。\n' >&2
            return 1
        fi
    else
        service_path="${S12RYT_OPENRC_SERVICE_PATH:-/etc/init.d/s12ryt-ipv6}"
        logrotate_path="${S12RYT_LOGROTATE_PATH:-/etc/logrotate.d/s12ryt-ipv6}"
        if ! write_ipv6_project_file "$service_path" 0755 "$(openrc_service_content "$project_root")" ||
            ! write_ipv6_project_file "$logrotate_path" 0644 "$(openrc_logrotate_content)" ||
            ! rc-update add s12ryt-ipv6 default ||
            ! rc-service s12ryt-ipv6 start; then
            rc-update del s12ryt-ipv6 default >/dev/null 2>&1 || true
            rm -f -- "$service_path" "$logrotate_path"
            cleanup_new_ipv6_installation "$project_root" "$((had_state == 0 ? 1 : 0))"
            printf '錯誤：無法註冊 OpenRC 服務。\n' >&2
            return 1
        fi
    fi

    printf '多 IPv6 出站面板安裝完成。\n'
}

install_ipv6_project_release() {
    local machine_arch architecture init_system temporary_root temporary_bundle install_status=0

    check_ipv6_project_preflight || return 1
    machine_arch="${S12RYT_MACHINE_ARCH:-$(uname -m)}"
    architecture="$(map_ipv6_project_arch "$machine_arch")" || return 1
    init_system="$(detect_ipv6_project_init)"
    temporary_root="$(mktemp -d "${TMPDIR:-/tmp}/s12ryt-ipv6-install.XXXXXX")" || {
        printf '錯誤：無法建立 IPv6 專案安裝暫存目錄。\n' >&2
        return 1
    }
    temporary_bundle="${temporary_root}/s12ryt-ipv6-bundle.assets"

    if fetch_ipv6_release_bundle "$temporary_bundle" "$architecture" &&
        install_verified_ipv6_bundle "$temporary_bundle" "$architecture" "$init_system"; then
        install_status=0
    else
        install_status=$?
    fi
    rm -rf -- "$temporary_root"
    return "$install_status"
}

configure_ipv6_project_state() {
    local project_root panel_binary

    check_ipv6_project_preflight || return 1
    project_root="${S12RYT_PROJECT_ROOT:-/opt/s12ryt-ipv6}"
    panel_binary="${project_root}/bin/s12ryt-ipv6"
    if [[ ! -x "$panel_binary" ]]; then
        printf '錯誤：IPv6 管理面板尚未安裝。\n' >&2
        return 1
    fi
    S12RYT_PROJECT_ROOT="$project_root" "$panel_binary" status
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
        install)
            if (($# != 1)); then
                printf '用法：%s install\n' "${0##*/}" >&2
                return 1
            fi
            install_ipv6_project_release
            ;;
        configure)
            if (($# != 1)); then
                printf '用法：%s configure\n' "${0##*/}" >&2
                return 1
            fi
            configure_ipv6_project_state
            ;;
        *)
            printf '用法：%s [preflight|fetch DESTINATION ARCH|install|configure]\n' "${0##*/}" >&2
            return 1
            ;;
    esac
}

if [[ "${BASH_SOURCE[0]}" == "$0" && "${S12RYT_IPV6_SOURCE_ONLY:-0}" != "1" ]]; then
    main "$@"
fi
