#!/usr/bin/env bats

load test_helper

setup() {
    setup_sandbox
    setup_mock_bin
    export PATH="${MOCK_BIN}:/usr/bin:/bin"
    export S12RYT_IPV6_SOURCE_ONLY=1
    export S12RYT_PROJECT_ROOT="${TEST_ROOT}/opt/s12ryt-ipv6"
}

create_ipv6_curl_mock() {
    cat > "${MOCK_BIN}/curl" <<'EOF'
#!/bin/bash
set -eu

printf 'curl %s\n' "$*" >> "$MOCK_LOG"

output=""
url=""
while (($# > 0)); do
    case "$1" in
        -o)
            output="$2"
            shift 2
            ;;
        http://*|https://*)
            url="$1"
            shift
            ;;
        *)
            shift
            ;;
    esac
done

case "${S12RYT_CURL_MODE:-success}:${url}" in
    download-fail:*)
        exit 22
        ;;
    *:*/SHA256SUMS)
        if [[ "${S12RYT_CURL_MODE:-success}" == "checksum-fail" ]]; then
            printf '%064d  s12ryt-ipv6-linux-amd64\n' 0 > "$output"
        else
            panel_file="${output%/*}/s12ryt-ipv6-linux-amd64"
            panel_hash="$(/usr/bin/sha256sum "$panel_file")"
            printf '%s  s12ryt-ipv6-linux-amd64\n' "${panel_hash%% *}" > "$output"
        fi
        ;;
    *:*/s12ryt-ipv6-linux-amd64)
        printf 'panel-amd64\n' > "$output"
        ;;
    *:*/sing-box-1.13.15-linux-amd64.tar.gz)
        if [[ "${S12RYT_CURL_MODE:-success}" == "archive-fail" ]]; then
            printf 'not-an-archive\n' > "$output"
        else
            archive_root="${output%/*}/archive-root"
            mkdir -p "${archive_root}/sing-box-1.13.15-linux-amd64"
            printf 'sing-box-amd64\n' > "${archive_root}/sing-box-1.13.15-linux-amd64/sing-box"
            /usr/bin/tar --sort=name --mtime='UTC 1970-01-01' --owner=0 --group=0 \
                --numeric-owner -czf "$output" -C "$archive_root" sing-box-1.13.15-linux-amd64
            rm -rf "$archive_root"
        fi
        ;;
    *)
        printf 'unexpected URL: %s\n' "$url" >&2
        exit 2
        ;;
esac
EOF
    chmod +x "${MOCK_BIN}/curl"
}

create_verified_ipv6_bundle() {
    local bundle="$1"
    local archive_root="${TEST_ROOT}/install-archive-root"

    mkdir -p "$bundle" "${archive_root}/sing-box-1.13.15-linux-amd64"
    cat > "${bundle}/s12ryt-ipv6-linux-amd64" <<'EOF'
#!/bin/bash
printf 'panel %s\n' "$*" >> "$MOCK_LOG"
if [[ "${1:-}" == "init" ]]; then
    mkdir -p "${S12RYT_PROJECT_ROOT}/config" "${S12RYT_PROJECT_ROOT}/secrets"
    printf '{"schema_version":1}\n' > "${S12RYT_PROJECT_ROOT}/config/config.json"
    printf 'protected-hash\n' > "${S12RYT_PROJECT_ROOT}/secrets/password.hash"
    printf 'GeneratedPassword12345678\n' > "${S12RYT_PROJECT_ROOT}/secrets/management.password"
fi
if [[ "${1:-}" == "health-url" ]]; then
    printf 'http://127.0.0.1:34456/abcdefghijkl/healthz\n'
fi
EOF
    chmod +x "${bundle}/s12ryt-ipv6-linux-amd64"
    cat > "${archive_root}/sing-box-1.13.15-linux-amd64/sing-box" <<'EOF'
#!/bin/bash
printf 'sing-box %s\n' "$*" >> "$MOCK_LOG"
EOF
    chmod +x "${archive_root}/sing-box-1.13.15-linux-amd64/sing-box"
    /usr/bin/tar -czf "${bundle}/sing-box-1.13.15-linux-amd64.tar.gz" \
        -C "$archive_root" sing-box-1.13.15-linux-amd64
    rm -rf "$archive_root"
}

create_installed_ipv6_project() {
    mkdir -p "${S12RYT_PROJECT_ROOT}/bin" "${S12RYT_PROJECT_ROOT}/config" \
        "${S12RYT_PROJECT_ROOT}/secrets"
    cat > "${S12RYT_PROJECT_ROOT}/bin/s12ryt-ipv6" <<'EOF'
#!/bin/bash
printf 'old-panel %s\n' "$*" >> "$MOCK_LOG"
EOF
    cat > "${S12RYT_PROJECT_ROOT}/bin/sing-box" <<'EOF'
#!/bin/bash
printf 'old-sing-box %s\n' "$*" >> "$MOCK_LOG"
EOF
    chmod 0755 "${S12RYT_PROJECT_ROOT}/bin/s12ryt-ipv6" "${S12RYT_PROJECT_ROOT}/bin/sing-box"
    printf '{"schema_version":1,"sentinel":"old-config"}\n' > "${S12RYT_PROJECT_ROOT}/config/config.json"
    printf '{"log":{"level":"warn"}}\n' > "${S12RYT_PROJECT_ROOT}/config/sing-box.json"
    printf 'protected-hash\n' > "${S12RYT_PROJECT_ROOT}/secrets/password.hash"
    printf 'GeneratedPassword12345678\n' > "${S12RYT_PROJECT_ROOT}/secrets/management.password"
    chmod 0600 "${S12RYT_PROJECT_ROOT}/config/config.json" \
        "${S12RYT_PROJECT_ROOT}/config/sing-box.json" \
        "${S12RYT_PROJECT_ROOT}/secrets/password.hash" \
        "${S12RYT_PROJECT_ROOT}/secrets/management.password"
}

create_ipv6_health_curl_mock() {
    cat > "${MOCK_BIN}/curl" <<'EOF'
#!/bin/bash
set -eu
printf 'curl %s\n' "$*" >> "$MOCK_LOG"
if [[ "${S12RYT_HEALTH_MODE:-success}" == "fail" ]]; then
    exit 22
fi
printf '{"status":"ok"}\n'
EOF
    chmod 0755 "${MOCK_BIN}/curl"
}

@test "IPv6 專案只接受 Linux root systemd 或 OpenRC 與支援架構" {
    local row kernel uid init arch expected
    local -a cases=(
        "Darwin|0|systemd|x86_64|僅支援 Linux"
        "Linux|1000|systemd|x86_64|需要 root 權限"
        "Linux|0|unknown|x86_64|僅支援 systemd 或 OpenRC"
        "Linux|0|systemd|mips64|不支援的架構"
    )

    for row in "${cases[@]}"; do
        IFS='|' read -r kernel uid init arch expected <<< "$row"
        run /usr/bin/env \
            S12RYT_KERNEL_NAME="$kernel" \
            S12RYT_EFFECTIVE_UID="$uid" \
            S12RYT_INIT_SYSTEM="$init" \
            S12RYT_MACHINE_ARCH="$arch" \
            /bin/bash -c 'source "$1"; check_ipv6_project_preflight' _ \
            "${PROJECT_ROOT}/install-ipv6.sh"

        [ "$status" -ne 0 ]
        [[ "$output" == *"$expected"* ]]
    done

    run /usr/bin/env \
        S12RYT_KERNEL_NAME=Linux \
        S12RYT_EFFECTIVE_UID=0 \
        S12RYT_INIT_SYSTEM=openrc \
        S12RYT_MACHINE_ARCH=aarch64 \
        /bin/bash -c 'source "$1"; check_ipv6_project_preflight' _ \
        "${PROJECT_ROOT}/install-ipv6.sh"

    [ "$status" -eq 0 ]
}

@test "架構與固定 Release 資產 metadata 精確映射" {
    run /usr/bin/env S12RYT_IPV6_SOURCE_ONLY=1 /bin/bash -c '
        source "$1"
        printf "%s\n" "$(map_ipv6_project_arch x86_64)"
        printf "%s\n" "$(map_ipv6_project_arch arm64)"
        printf "%s\n" "$(panel_asset_name amd64)"
        printf "%s\n" "$(panel_asset_name arm64)"
        printf "%s\n" "$(singbox_asset_name amd64)"
        printf "%s\n" "$(singbox_asset_name arm64)"
        printf "%s\n" "$(singbox_asset_digest amd64)"
        printf "%s\n" "$(singbox_asset_digest arm64)"
    ' _ "${PROJECT_ROOT}/install-ipv6.sh"

    [ "$status" -eq 0 ]
    [[ "$output" == *$'amd64\narm64'* ]]
    [[ "$output" == *"s12ryt-ipv6-linux-amd64"* ]]
    [[ "$output" == *"s12ryt-ipv6-linux-arm64"* ]]
    [[ "$output" == *"sing-box-1.13.15-linux-amd64.tar.gz"* ]]
    [[ "$output" == *"sing-box-1.13.15-linux-arm64.tar.gz"* ]]
    [[ "$output" == *"a3a3ff223b23c3f4731d0a17cb0ef94c97ce257c70721a5b07dc7ca079203c9f"* ]]
    [[ "$output" == *"f0810bbb5722ae36635687c421019defcc8b328d31a0b3c287901f331747ca93"* ]]
}

@test "下載面板與 sing-box 固定資產並完成雙重 SHA256 和壓縮檔驗證" {
    create_ipv6_curl_mock
    local bundle="${TEST_ROOT}/bundle"
    local fixture_archive="${TEST_ROOT}/fixture-sing-box.tar.gz"

    S12RYT_CURL_MODE=success "${MOCK_BIN}/curl" \
        https://github.com/SagerNet/sing-box/releases/download/v1.13.15/sing-box-1.13.15-linux-amd64.tar.gz \
        -o "$fixture_archive"
    local fixture_hash
    fixture_hash="$(/usr/bin/sha256sum "$fixture_archive")"
    : > "$MOCK_LOG"

    run /usr/bin/env \
        PATH="$PATH" \
        MOCK_LOG="$MOCK_LOG" \
        S12RYT_CURL_MODE=success \
        S12RYT_SINGBOX_SHA256="${fixture_hash%% *}" \
        /bin/bash -c 'source "$1"; fetch_ipv6_release_bundle "$2" amd64' _ \
        "${PROJECT_ROOT}/install-ipv6.sh" "$bundle"

    [ "$status" -eq 0 ]
    [ "$(cat "${bundle}/s12ryt-ipv6-linux-amd64")" = "panel-amd64" ]
    [ -s "${bundle}/SHA256SUMS" ]
    [ -s "${bundle}/sing-box-1.13.15-linux-amd64.tar.gz" ]
    grep -Fq -- '--connect-timeout 5 --max-time 60' "$MOCK_LOG"
    grep -Fq 'https://github.com/s12ryt/s12ryt-vps-sh/releases/download/v1.1.0/s12ryt-ipv6-linux-amd64' "$MOCK_LOG"
    grep -Fq 'https://github.com/s12ryt/s12ryt-vps-sh/releases/download/v1.1.0/SHA256SUMS' "$MOCK_LOG"
    grep -Fq 'https://github.com/SagerNet/sing-box/releases/download/v1.13.15/sing-box-1.13.15-linux-amd64.tar.gz' "$MOCK_LOG"
}

@test "資產下載校驗或壓縮檔失敗時保留既有目標並清理暫存" {
    create_ipv6_curl_mock
    local mode expected bundle singbox_digest

    while IFS='|' read -r mode expected; do
        bundle="${TEST_ROOT}/bundle-${mode}"
        mkdir -p "$bundle"
        printf 'existing-sentinel\n' > "${bundle}/sentinel"
        singbox_digest=invalid-fixture-digest
        if [[ "$mode" == "archive-fail" ]]; then
            printf 'not-an-archive\n' > "${TEST_ROOT}/invalid-archive"
            singbox_digest="$(/usr/bin/sha256sum "${TEST_ROOT}/invalid-archive")"
            singbox_digest="${singbox_digest%% *}"
        fi

        run /usr/bin/env \
            PATH="$PATH" \
            MOCK_LOG="$MOCK_LOG" \
            S12RYT_CURL_MODE="$mode" \
            S12RYT_SINGBOX_SHA256="$singbox_digest" \
            /bin/bash -c 'source "$1"; fetch_ipv6_release_bundle "$2" amd64' _ \
            "${PROJECT_ROOT}/install-ipv6.sh" "$bundle"

        [ "$status" -ne 0 ]
        [[ "$output" == *"$expected"* ]]
        [ "$(cat "${bundle}/sentinel")" = "existing-sentinel" ]
        [ "$(find "${TEST_ROOT}" -maxdepth 1 -name '.s12ryt-ipv6-download.*' | wc -l)" -eq 0 ]
    done <<'EOF'
download-fail|無法下載 IPv6 專案資產
checksum-fail|面板資產 SHA256 驗證失敗
archive-fail|sing-box 壓縮檔驗證失敗
EOF
}

@test "systemd 安裝會部署受保護 binary 初始化狀態並啟用服務" {
    create_command_mock systemctl
    local bundle="${TEST_ROOT}/verified-bundle"
    create_verified_ipv6_bundle "$bundle"
    local unit_path="${TEST_ROOT}/etc/systemd/system/s12ryt-ipv6.service"
    local network_unit_path="${TEST_ROOT}/etc/systemd/system/s12ryt-ipv6-network.service"

    run /usr/bin/env \
        PATH="$PATH" \
        MOCK_LOG="$MOCK_LOG" \
        S12RYT_PROJECT_ROOT="$S12RYT_PROJECT_ROOT" \
        S12RYT_SYSTEMD_UNIT_PATH="$unit_path" \
        S12RYT_SYSTEMD_NETWORK_UNIT_PATH="$network_unit_path" \
        /bin/bash -c 'source "$1"; install_verified_ipv6_bundle "$2" amd64 systemd' _ \
        "${PROJECT_ROOT}/install-ipv6.sh" "$bundle"

    [ "$status" -eq 0 ]
    [ -x "${S12RYT_PROJECT_ROOT}/bin/s12ryt-ipv6" ]
    [ -x "${S12RYT_PROJECT_ROOT}/bin/sing-box" ]
    [ -s "${S12RYT_PROJECT_ROOT}/config/config.json" ]
    [ "$(stat -c '%a' "${S12RYT_PROJECT_ROOT}/secrets/password.hash")" = "600" ]
    [ "$(stat -c '%a' "${S12RYT_PROJECT_ROOT}/secrets/management.password")" = "600" ]
    [ "$(stat -c '%a' "$unit_path")" = "644" ]
    [ "$(stat -c '%a' "$network_unit_path")" = "644" ]
    grep -Fq "ExecStart=${S12RYT_PROJECT_ROOT}/bin/s12ryt-ipv6 serve" "$unit_path"
    grep -Fq 'Requires=s12ryt-ipv6-network.service' "$unit_path"
    grep -Fq 'NoNewPrivileges=true' "$unit_path"
    grep -Fq "ReadWritePaths=${S12RYT_PROJECT_ROOT}" "$unit_path"
    grep -Fq 'Type=oneshot' "$network_unit_path"
    grep -Fq "ExecStart=${S12RYT_PROJECT_ROOT}/bin/s12ryt-ipv6 restore-system" "$network_unit_path"
    grep -Fq 'RemainAfterExit=yes' "$network_unit_path"
    grep -Fxq 'systemctl daemon-reload' "$MOCK_LOG"
    grep -Fxq 'systemctl enable s12ryt-ipv6-network.service' "$MOCK_LOG"
    grep -Fxq 'systemctl enable --now s12ryt-ipv6.service' "$MOCK_LOG"
    grep -Fxq 'panel init' "$MOCK_LOG"
}

@test "configure 命令只透過已安裝 binary 顯示受保護狀態" {
    mkdir -p "${S12RYT_PROJECT_ROOT}/bin"
    cat > "${S12RYT_PROJECT_ROOT}/bin/s12ryt-ipv6" <<'EOF'
#!/bin/bash
printf 'panel %s\n' "$*" >> "$MOCK_LOG"
EOF
    chmod 0755 "${S12RYT_PROJECT_ROOT}/bin/s12ryt-ipv6"

    run /usr/bin/env \
        PATH="$PATH" \
        MOCK_LOG="$MOCK_LOG" \
        S12RYT_PROJECT_ROOT="$S12RYT_PROJECT_ROOT" \
        S12RYT_KERNEL_NAME=Linux \
        S12RYT_EFFECTIVE_UID=0 \
        S12RYT_INIT_SYSTEM=systemd \
        S12RYT_MACHINE_ARCH=x86_64 \
        S12RYT_IPV6_SOURCE_ONLY=0 \
        /bin/bash "${PROJECT_ROOT}/install-ipv6.sh" configure

    [ "$status" -eq 0 ]
    grep -Fxq 'panel status' "$MOCK_LOG"

    rm -f "${S12RYT_PROJECT_ROOT}/bin/s12ryt-ipv6"
    run /usr/bin/env \
        PATH="$PATH" \
        S12RYT_PROJECT_ROOT="$S12RYT_PROJECT_ROOT" \
        S12RYT_KERNEL_NAME=Linux \
        S12RYT_EFFECTIVE_UID=0 \
        S12RYT_INIT_SYSTEM=systemd \
        S12RYT_MACHINE_ARCH=x86_64 \
        S12RYT_IPV6_SOURCE_ONLY=0 \
        /bin/bash "${PROJECT_ROOT}/install-ipv6.sh" configure

    [ "$status" -ne 0 ]
    [[ "$output" == *"IPv6 管理面板尚未安裝"* ]]
}

@test "下載後尚未帶執行權限的面板資產仍可安全部署" {
    create_command_mock systemctl
    local bundle="${TEST_ROOT}/verified-bundle"
    create_verified_ipv6_bundle "$bundle"
    chmod 0644 "${bundle}/s12ryt-ipv6-linux-amd64"
    local unit_path="${TEST_ROOT}/etc/systemd/system/s12ryt-ipv6.service"
    local network_unit_path="${TEST_ROOT}/etc/systemd/system/s12ryt-ipv6-network.service"

    run /usr/bin/env \
        PATH="$PATH" \
        MOCK_LOG="$MOCK_LOG" \
        S12RYT_PROJECT_ROOT="$S12RYT_PROJECT_ROOT" \
        S12RYT_SYSTEMD_UNIT_PATH="$unit_path" \
        S12RYT_SYSTEMD_NETWORK_UNIT_PATH="$network_unit_path" \
        /bin/bash -c 'source "$1"; install_verified_ipv6_bundle "$2" amd64 systemd' _ \
        "${PROJECT_ROOT}/install-ipv6.sh" "$bundle"

    [ "$status" -eq 0 ]
    [ -x "${S12RYT_PROJECT_ROOT}/bin/s12ryt-ipv6" ]
    grep -Fxq 'panel init' "$MOCK_LOG"
}

@test "OpenRC 安裝會建立服務與受限輪替日誌設定" {
    create_command_mock rc-update
    create_command_mock rc-service
    local bundle="${TEST_ROOT}/verified-bundle"
    create_verified_ipv6_bundle "$bundle"
    local service_path="${TEST_ROOT}/etc/init.d/s12ryt-ipv6"
    local network_service_path="${TEST_ROOT}/etc/init.d/s12ryt-ipv6-network"
    local logrotate_path="${TEST_ROOT}/etc/logrotate.d/s12ryt-ipv6"

    run /usr/bin/env \
        PATH="$PATH" \
        MOCK_LOG="$MOCK_LOG" \
        S12RYT_PROJECT_ROOT="$S12RYT_PROJECT_ROOT" \
        S12RYT_OPENRC_SERVICE_PATH="$service_path" \
        S12RYT_OPENRC_NETWORK_SERVICE_PATH="$network_service_path" \
        S12RYT_LOGROTATE_PATH="$logrotate_path" \
        /bin/bash -c 'source "$1"; install_verified_ipv6_bundle "$2" amd64 openrc' _ \
        "${PROJECT_ROOT}/install-ipv6.sh" "$bundle"

    [ "$status" -eq 0 ]
    [ "$(stat -c '%a' "$service_path")" = "755" ]
    [ "$(stat -c '%a' "$network_service_path")" = "755" ]
    [ "$(stat -c '%a' "$logrotate_path")" = "644" ]
    grep -Fq "command=\"${S12RYT_PROJECT_ROOT}/bin/s12ryt-ipv6\"" "$service_path"
    grep -Fq 'command_args="serve"' "$service_path"
    grep -Fq 'need s12ryt-ipv6-network' "$service_path"
    grep -Fq "command=\"${S12RYT_PROJECT_ROOT}/bin/s12ryt-ipv6\"" "$network_service_path"
    grep -Fq 'command_args="restore-system"' "$network_service_path"
    grep -Fq 'rotate 7' "$logrotate_path"
    grep -Fq 'size 100M' "$logrotate_path"
    grep -Fxq 'rc-update add s12ryt-ipv6-network default' "$MOCK_LOG"
    grep -Fxq 'rc-update add s12ryt-ipv6 default' "$MOCK_LOG"
    grep -Fxq 'rc-service s12ryt-ipv6-network start' "$MOCK_LOG"
    grep -Fxq 'rc-service s12ryt-ipv6 start' "$MOCK_LOG"
}

@test "初始化失敗時不註冊服務並移除本次新安裝檔案" {
    create_command_mock systemctl
    local bundle="${TEST_ROOT}/verified-bundle"
    create_verified_ipv6_bundle "$bundle"
    cat > "${bundle}/s12ryt-ipv6-linux-amd64" <<'EOF'
#!/bin/bash
printf 'panel %s\n' "$*" >> "$MOCK_LOG"
exit 1
EOF
    chmod +x "${bundle}/s12ryt-ipv6-linux-amd64"
    local unit_path="${TEST_ROOT}/etc/systemd/system/s12ryt-ipv6.service"

    run /usr/bin/env \
        PATH="$PATH" \
        MOCK_LOG="$MOCK_LOG" \
        S12RYT_PROJECT_ROOT="$S12RYT_PROJECT_ROOT" \
        S12RYT_SYSTEMD_UNIT_PATH="$unit_path" \
        /bin/bash -c 'source "$1"; install_verified_ipv6_bundle "$2" amd64 systemd' _ \
        "${PROJECT_ROOT}/install-ipv6.sh" "$bundle"

    [ "$status" -ne 0 ]
    [[ "$output" == *"面板初始化失敗"* ]]
    [ ! -e "${S12RYT_PROJECT_ROOT}/bin/s12ryt-ipv6" ]
    [ ! -e "${S12RYT_PROJECT_ROOT}/bin/sing-box" ]
    [ ! -e "$unit_path" ]
    ! grep -Fq 'enable --now' "$MOCK_LOG"
}

@test "install 命令依序完成前置檢查下載與服務部署" {
    run /usr/bin/env \
        TRACE_LOG="$MOCK_LOG" \
        TMPDIR="$TEST_ROOT" \
        /bin/bash -c '
            source "$1"
            check_ipv6_project_preflight() { printf "preflight\n" >> "$TRACE_LOG"; }
            map_ipv6_project_arch() { printf "amd64\n"; }
            detect_ipv6_project_init() { printf "systemd\n"; }
            fetch_ipv6_release_bundle() {
                printf "fetch %s %s\n" "$1" "$2" >> "$TRACE_LOG"
                mkdir -p "$1"
            }
            install_verified_ipv6_bundle() {
                printf "install %s %s %s\n" "$1" "$2" "$3" >> "$TRACE_LOG"
            }
            main install
        ' _ "${PROJECT_ROOT}/install-ipv6.sh"

    [ "$status" -eq 0 ]
    [ "$(sed -n '1p' "$MOCK_LOG")" = "preflight" ]
    grep -Eq '^fetch .*/s12ryt-ipv6-bundle\.[^ ]+ amd64$' "$MOCK_LOG"
    grep -Eq '^install .*/s12ryt-ipv6-bundle\.[^ ]+ amd64 systemd$' "$MOCK_LOG"
    [ "$(find "$TEST_ROOT" -maxdepth 1 -name 's12ryt-ipv6-bundle.*' | wc -l)" -eq 0 ]
}

@test "systemd 更新會備份狀態驗證 sing-box 並通過面板健康檢查" {
    create_installed_ipv6_project
    create_command_mock systemctl
    create_ipv6_health_curl_mock
    local bundle="${TEST_ROOT}/update-bundle"
    create_verified_ipv6_bundle "$bundle"

    run /usr/bin/env \
        PATH="$PATH" \
        MOCK_LOG="$MOCK_LOG" \
        S12RYT_PROJECT_ROOT="$S12RYT_PROJECT_ROOT" \
        /bin/bash -c 'source "$1"; update_verified_ipv6_bundle "$2" amd64 systemd' _ \
        "${PROJECT_ROOT}/install-ipv6.sh" "$bundle"

    [ "$status" -eq 0 ]
    grep -Fq 'panel %s' "${S12RYT_PROJECT_ROOT}/bin/s12ryt-ipv6"
    grep -Fq 'sing-box %s' "${S12RYT_PROJECT_ROOT}/bin/sing-box"
    grep -Fq 'old-config' "${S12RYT_PROJECT_ROOT}/config/config.json"
    local backup
    backup="$(find "${S12RYT_PROJECT_ROOT}/backups" -mindepth 1 -maxdepth 1 -type d | head -n 1)"
    [ -n "$backup" ]
    grep -Fq 'old-panel %s' "${backup}/bin/s12ryt-ipv6"
    grep -Fq 'old-sing-box %s' "${backup}/bin/sing-box"
    grep -Fq 'old-config' "${backup}/config/config.json"
    grep -Fxq 'sing-box check -c '"${S12RYT_PROJECT_ROOT}"'/config/sing-box.json' "$MOCK_LOG"
    grep -Fxq 'systemctl restart s12ryt-ipv6.service' "$MOCK_LOG"
    grep -Fxq 'panel health-url' "$MOCK_LOG"
    grep -Fq -- '--connect-timeout 2 --max-time 10 --retry 5 --retry-delay 1 http://127.0.0.1:34456/abcdefghijkl/healthz' "$MOCK_LOG"
}

@test "更新健康檢查失敗時會恢復舊 binary 設定並重啟舊服務" {
    create_installed_ipv6_project
    create_command_mock systemctl
    create_ipv6_health_curl_mock
    local bundle="${TEST_ROOT}/failed-update-bundle"
    create_verified_ipv6_bundle "$bundle"
    local old_panel_hash old_singbox_hash old_config_hash
    old_panel_hash="$(sha256sum "${S12RYT_PROJECT_ROOT}/bin/s12ryt-ipv6")"
    old_singbox_hash="$(sha256sum "${S12RYT_PROJECT_ROOT}/bin/sing-box")"
    old_config_hash="$(sha256sum "${S12RYT_PROJECT_ROOT}/config/config.json")"

    run /usr/bin/env \
        PATH="$PATH" \
        MOCK_LOG="$MOCK_LOG" \
        S12RYT_PROJECT_ROOT="$S12RYT_PROJECT_ROOT" \
        S12RYT_HEALTH_MODE=fail \
        /bin/bash -c 'source "$1"; update_verified_ipv6_bundle "$2" amd64 systemd' _ \
        "${PROJECT_ROOT}/install-ipv6.sh" "$bundle"

    [ "$status" -ne 0 ]
    [[ "$output" == *"健康檢查失敗，已恢復舊版本"* ]]
    [ "$(sha256sum "${S12RYT_PROJECT_ROOT}/bin/s12ryt-ipv6")" = "$old_panel_hash" ]
    [ "$(sha256sum "${S12RYT_PROJECT_ROOT}/bin/sing-box")" = "$old_singbox_hash" ]
    [ "$(sha256sum "${S12RYT_PROJECT_ROOT}/config/config.json")" = "$old_config_hash" ]
    [ "$(grep -Fxc 'systemctl restart s12ryt-ipv6.service' "$MOCK_LOG")" -eq 2 ]
}

@test "systemd 卸載可保留設定機密與備份但移除服務和 binary" {
    create_installed_ipv6_project
    create_command_mock systemctl
    mkdir -p "${S12RYT_PROJECT_ROOT}/backups/update.saved"
    printf 'backup-sentinel\n' > "${S12RYT_PROJECT_ROOT}/backups/update.saved/sentinel"
    local unit_path="${TEST_ROOT}/etc/systemd/system/s12ryt-ipv6.service"
    local network_unit_path="${TEST_ROOT}/etc/systemd/system/s12ryt-ipv6-network.service"
    mkdir -p "${unit_path%/*}"
    printf 'unit-sentinel\n' > "$unit_path"
    printf 'network-unit-sentinel\n' > "$network_unit_path"

    run /usr/bin/env \
        PATH="$PATH" \
        MOCK_LOG="$MOCK_LOG" \
        S12RYT_PROJECT_ROOT="$S12RYT_PROJECT_ROOT" \
        S12RYT_SYSTEMD_UNIT_PATH="$unit_path" \
        S12RYT_SYSTEMD_NETWORK_UNIT_PATH="$network_unit_path" \
        /bin/bash -c 'source "$1"; printf "1\ny\n" | uninstall_ipv6_project_state systemd' _ \
        "${PROJECT_ROOT}/install-ipv6.sh"

    [ "$status" -eq 0 ]
    [ ! -e "${S12RYT_PROJECT_ROOT}/bin/s12ryt-ipv6" ]
    [ ! -e "${S12RYT_PROJECT_ROOT}/bin/sing-box" ]
    [ ! -e "$unit_path" ]
    [ ! -e "$network_unit_path" ]
    [ -s "${S12RYT_PROJECT_ROOT}/config/config.json" ]
    [ -s "${S12RYT_PROJECT_ROOT}/secrets/management.password" ]
    [ -s "${S12RYT_PROJECT_ROOT}/backups/update.saved/sentinel" ]
    [ "$(head -n 1 "$MOCK_LOG")" = "old-panel cleanup-system" ]
    grep -Fxq 'systemctl disable --now s12ryt-ipv6.service' "$MOCK_LOG"
    grep -Fxq 'systemctl disable --now s12ryt-ipv6-network.service' "$MOCK_LOG"
    grep -Fxq 'systemctl daemon-reload' "$MOCK_LOG"
}

@test "OpenRC 完整卸載會反註冊服務並刪除全部專案資料" {
    create_installed_ipv6_project
    create_command_mock rc-update
    create_command_mock rc-service
    local service_path="${TEST_ROOT}/etc/init.d/s12ryt-ipv6"
    local network_service_path="${TEST_ROOT}/etc/init.d/s12ryt-ipv6-network"
    local logrotate_path="${TEST_ROOT}/etc/logrotate.d/s12ryt-ipv6"
    mkdir -p "${service_path%/*}" "${logrotate_path%/*}"
    printf 'service-sentinel\n' > "$service_path"
    printf 'network-service-sentinel\n' > "$network_service_path"
    printf 'logrotate-sentinel\n' > "$logrotate_path"

    run /usr/bin/env \
        PATH="$PATH" \
        MOCK_LOG="$MOCK_LOG" \
        S12RYT_PROJECT_ROOT="$S12RYT_PROJECT_ROOT" \
        S12RYT_OPENRC_SERVICE_PATH="$service_path" \
        S12RYT_OPENRC_NETWORK_SERVICE_PATH="$network_service_path" \
        S12RYT_LOGROTATE_PATH="$logrotate_path" \
        /bin/bash -c 'source "$1"; printf "2\ny\n" | uninstall_ipv6_project_state openrc' _ \
        "${PROJECT_ROOT}/install-ipv6.sh"

    [ "$status" -eq 0 ]
    [ ! -e "$S12RYT_PROJECT_ROOT" ]
    [ ! -e "$service_path" ]
    [ ! -e "$network_service_path" ]
    [ ! -e "$logrotate_path" ]
    [ "$(head -n 1 "$MOCK_LOG")" = "old-panel cleanup-system" ]
    grep -Fxq 'rc-service s12ryt-ipv6 stop' "$MOCK_LOG"
    grep -Fxq 'rc-service s12ryt-ipv6-network stop' "$MOCK_LOG"
    grep -Fxq 'rc-update del s12ryt-ipv6 default' "$MOCK_LOG"
    grep -Fxq 'rc-update del s12ryt-ipv6-network default' "$MOCK_LOG"
}

@test "系統整合清理失敗時停止卸載並保留服務與資料" {
    create_installed_ipv6_project
    cat > "${S12RYT_PROJECT_ROOT}/bin/s12ryt-ipv6" <<'EOF'
#!/bin/bash
printf 'old-panel %s\n' "$*" >> "$MOCK_LOG"
if [[ "${1:-}" == "cleanup-system" ]]; then
    exit 1
fi
EOF
    chmod 0755 "${S12RYT_PROJECT_ROOT}/bin/s12ryt-ipv6"
    create_command_mock systemctl
    local unit_path="${TEST_ROOT}/etc/systemd/system/s12ryt-ipv6.service"
    local network_unit_path="${TEST_ROOT}/etc/systemd/system/s12ryt-ipv6-network.service"
    mkdir -p "${unit_path%/*}" "${S12RYT_PROJECT_ROOT}/state"
    printf 'unit-sentinel\n' > "$unit_path"
    printf 'network-unit-sentinel\n' > "$network_unit_path"
    printf 'manifest-sentinel\n' > "${S12RYT_PROJECT_ROOT}/state/integration.json"

    run /usr/bin/env \
        PATH="$PATH" \
        MOCK_LOG="$MOCK_LOG" \
        S12RYT_PROJECT_ROOT="$S12RYT_PROJECT_ROOT" \
        S12RYT_SYSTEMD_UNIT_PATH="$unit_path" \
        S12RYT_SYSTEMD_NETWORK_UNIT_PATH="$network_unit_path" \
        /bin/bash -c 'source "$1"; printf "2\ny\n" | uninstall_ipv6_project_state systemd' _ \
        "${PROJECT_ROOT}/install-ipv6.sh"

    [ "$status" -ne 0 ]
    [[ "$output" == *"系統整合狀態清理失敗，已停止卸載"* ]]
    [ -x "${S12RYT_PROJECT_ROOT}/bin/s12ryt-ipv6" ]
    [ -s "${S12RYT_PROJECT_ROOT}/state/integration.json" ]
    [ -s "$unit_path" ]
    [ -s "$network_unit_path" ]
    [ "$(cat "$MOCK_LOG")" = "old-panel cleanup-system" ]
}

@test "拒絕卸載確認時完全不修改服務與專案資料" {
    create_installed_ipv6_project
    create_command_mock systemctl
    local unit_path="${TEST_ROOT}/etc/systemd/system/s12ryt-ipv6.service"
    local network_unit_path="${TEST_ROOT}/etc/systemd/system/s12ryt-ipv6-network.service"
    mkdir -p "${unit_path%/*}"
    printf 'unit-sentinel\n' > "$unit_path"
    printf 'network-unit-sentinel\n' > "$network_unit_path"

    run /usr/bin/env \
        PATH="$PATH" \
        MOCK_LOG="$MOCK_LOG" \
        S12RYT_PROJECT_ROOT="$S12RYT_PROJECT_ROOT" \
        S12RYT_SYSTEMD_UNIT_PATH="$unit_path" \
        S12RYT_SYSTEMD_NETWORK_UNIT_PATH="$network_unit_path" \
        /bin/bash -c 'source "$1"; printf "2\nn\n" | uninstall_ipv6_project_state systemd' _ \
        "${PROJECT_ROOT}/install-ipv6.sh"

    [ "$status" -eq 0 ]
    [[ "$output" == *"已取消多 IPv6 出站卸載"* ]]
    [ -x "${S12RYT_PROJECT_ROOT}/bin/s12ryt-ipv6" ]
    [ -s "$unit_path" ]
    [ -s "$network_unit_path" ]
    [ ! -s "$MOCK_LOG" ]
}
