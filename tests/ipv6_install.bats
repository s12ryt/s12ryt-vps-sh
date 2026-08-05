#!/usr/bin/env bats

load test_helper

setup() {
    setup_sandbox
    setup_mock_bin
    export PATH="${MOCK_BIN}:/usr/bin:/bin"
    export S12RYT_IPV6_SOURCE_ONLY=1
    export S12RYT_KERNEL_NAME=Linux
    export S12RYT_EFFECTIVE_UID=0
    export S12RYT_INIT_SYSTEM=systemd
    export S12RYT_MACHINE_ARCH=x86_64
    export S12RYT_OS_RELEASE_FILE="${TEST_ROOT}/os-release"
    export S12RYT_LEGACY_ROOT="${TEST_ROOT}/opt/s12ryt-ipv6"
    export S12RYT_SYSTEMD_UNIT_PATH="${TEST_ROOT}/etc/systemd/system/s12ryt-ipv6.service"
    export S12RYT_SYSTEMD_NETWORK_UNIT_PATH="${TEST_ROOT}/etc/systemd/system/s12ryt-ipv6-network.service"
    export S12RYT_NEW_DATA_DIR="${TEST_ROOT}/etc/s12ryt-ipv6"
    export S12RYT_RELEASE_API_URL="https://api.github.test/repos/s12ryt/s12ryt-ipv6/releases/latest"
    export S12RYT_RAW_BASE_URL="https://raw.github.test/s12ryt/s12ryt-ipv6"
    write_os_release debian 12
}

write_os_release() {
    local distribution="$1"
    local version="$2"

    cat > "$S12RYT_OS_RELEASE_FILE" <<EOF
ID=${distribution}
VERSION_ID="${version}"
EOF
}

create_curl_mock() {
    cat > "${MOCK_BIN}/curl" <<'EOF'
#!/bin/bash
set -u

printf 'curl %s\n' "$*" >> "$MOCK_LOG"
output=''
url=''
while (($# > 0)); do
    case "$1" in
        -o)
            output="$2"
            shift 2
            ;;
        http://* | https://*)
            url="$1"
            shift
            ;;
        *)
            shift
            ;;
    esac
done

if [[ "${S12RYT_CURL_MODE:-success}" == 'download-fail' ]]; then
    exit 22
fi

case "$url" in
    */releases/latest)
        case "${S12RYT_RELEASE_MODE:-stable}" in
            stable)
                printf '%s\n' '{"tag_name":"v0.1.8","draft":false,"prerelease":false}' > "$output"
                ;;
            draft)
                printf '%s\n' '{"tag_name":"v0.1.8","draft":true,"prerelease":false}' > "$output"
                ;;
            prerelease)
                printf '%s\n' '{"tag_name":"v0.1.8-rc.1","draft":false,"prerelease":true}' > "$output"
                ;;
            invalid-tag)
                printf '%s\n' '{"tag_name":"latest","draft":false,"prerelease":false}' > "$output"
                ;;
            malformed)
                printf '%s\n' '{not-json' > "$output"
                ;;
        esac
        ;;
    */v0.1.8/install.sh)
        if [[ "${S12RYT_SCRIPT_MODE:-valid}" == 'invalid-syntax' ]]; then
            printf '%s\n' 'if then' > "$output"
        else
            cat > "$output" <<'SCRIPT'
#!/bin/bash
printf 'upstream-install VERSION=%s MANAGEMENT_PORT=%s\n' \
    "${VERSION:-<unset>}" "${MANAGEMENT_PORT:-<unset>}" >> "$MOCK_LOG"
SCRIPT
        fi
        ;;
    */v0.1.8/deploy/uninstall.sh)
        cat > "$output" <<'SCRIPT'
#!/bin/bash
printf 'upstream-uninstall VERSION=%s\n' "${VERSION:-<unset>}" >> "$MOCK_LOG"
SCRIPT
        ;;
    *)
        printf 'unexpected URL: %s\n' "$url" >&2
        exit 2
        ;;
esac
EOF
    chmod 0755 "${MOCK_BIN}/curl"
}

create_systemctl_mock() {
    cat > "${MOCK_BIN}/systemctl" <<'EOF'
#!/bin/bash
set -u

printf 'systemctl %s\n' "$*" >> "$MOCK_LOG"
case "${1:-}" in
    is-enabled)
        [[ "${S12RYT_LEGACY_ENABLED:-1}" == '1' ]]
        ;;
    is-active)
        [[ "${S12RYT_LEGACY_ACTIVE:-1}" == '1' ]]
        ;;
    disable)
        if [[ "${S12RYT_SYSTEMCTL_FAIL:-}" == 'disable-network' && "${*: -1}" == 's12ryt-ipv6-network.service' ]]; then
            exit 1
        fi
        ;;
    daemon-reload)
        [[ "${S12RYT_SYSTEMCTL_FAIL:-}" != 'daemon-reload' ]]
        ;;
esac
EOF
    chmod 0755 "${MOCK_BIN}/systemctl"
}

create_legacy_file_operation_mocks() {
    cat > "${MOCK_BIN}/cp" <<'EOF'
#!/bin/bash
set -u

printf 'cp %s\n' "$*" >> "$MOCK_LOG"
if [[ "${S12RYT_FILE_OP_FAIL:-}" == 'backup-main' ]]; then
    for argument in "$@"; do
        if [[ "$argument" == "$S12RYT_SYSTEMD_UNIT_PATH" ]]; then
            exit 1
        fi
    done
fi
exec /bin/cp "$@"
EOF
    chmod 0755 "${MOCK_BIN}/cp"

    cat > "${MOCK_BIN}/rm" <<'EOF'
#!/bin/bash
set -u

printf 'rm %s\n' "$*" >> "$MOCK_LOG"
if [[ "${S12RYT_FILE_OP_FAIL:-}" == 'remove-units' ]]; then
    for argument in "$@"; do
        if [[ "$argument" == "$S12RYT_SYSTEMD_UNIT_PATH" ||
            "$argument" == "$S12RYT_SYSTEMD_NETWORK_UNIT_PATH" ]]; then
            exit 1
        fi
    done
fi
exec /bin/rm "$@"
EOF
    chmod 0755 "${MOCK_BIN}/rm"
}

create_legacy_installation() {
    mkdir -p "${S12RYT_LEGACY_ROOT}/bin" "${S12RYT_SYSTEMD_UNIT_PATH%/*}"
    cat > "${S12RYT_LEGACY_ROOT}/bin/s12ryt-ipv6" <<'EOF'
#!/bin/bash
printf 'legacy-panel %s\n' "$*" >> "$MOCK_LOG"
if [[ "${S12RYT_LEGACY_CLEANUP_FAIL:-0}" == '1' && "${1:-}" == 'cleanup-system' ]]; then
    exit 1
fi
EOF
    chmod 0755 "${S12RYT_LEGACY_ROOT}/bin/s12ryt-ipv6"
    printf 'legacy-data\n' > "${S12RYT_LEGACY_ROOT}/sentinel"
    printf 'legacy-main-unit\n' > "$S12RYT_SYSTEMD_UNIT_PATH"
    printf 'legacy-network-unit\n' > "$S12RYT_SYSTEMD_NETWORK_UNIT_PATH"
}

run_helper() {
    local action="$1"
    shift

    run /usr/bin/env \
        PATH="$PATH" \
        MOCK_LOG="$MOCK_LOG" \
        S12RYT_IPV6_SOURCE_ONLY=0 \
        S12RYT_KERNEL_NAME="$S12RYT_KERNEL_NAME" \
        S12RYT_EFFECTIVE_UID="$S12RYT_EFFECTIVE_UID" \
        S12RYT_INIT_SYSTEM="$S12RYT_INIT_SYSTEM" \
        S12RYT_MACHINE_ARCH="$S12RYT_MACHINE_ARCH" \
        S12RYT_OS_RELEASE_FILE="$S12RYT_OS_RELEASE_FILE" \
        S12RYT_LEGACY_ROOT="$S12RYT_LEGACY_ROOT" \
        S12RYT_SYSTEMD_UNIT_PATH="$S12RYT_SYSTEMD_UNIT_PATH" \
        S12RYT_SYSTEMD_NETWORK_UNIT_PATH="$S12RYT_SYSTEMD_NETWORK_UNIT_PATH" \
        S12RYT_NEW_DATA_DIR="$S12RYT_NEW_DATA_DIR" \
        S12RYT_RELEASE_API_URL="$S12RYT_RELEASE_API_URL" \
        S12RYT_RAW_BASE_URL="$S12RYT_RAW_BASE_URL" \
        "$@" \
        /bin/bash "${PROJECT_ROOT}/install-ipv6.sh" "$action"
}

@test "前置檢查只接受 Linux root systemd 支援架構與指定發行版" {
    local row kernel uid init arch distribution version expected
    local -a cases=(
        "Darwin|0|systemd|x86_64|debian|12|僅支援 Linux"
        "Linux|1000|systemd|x86_64|debian|12|需要 root 權限"
        "Linux|0|openrc|x86_64|debian|12|僅支援 systemd"
        "Linux|0|systemd|mips64|debian|12|不支援的架構"
        "Linux|0|systemd|x86_64|debian|11|僅支援 Debian 12/13 或 Ubuntu 24.04"
        "Linux|0|systemd|x86_64|ubuntu|22.04|僅支援 Debian 12/13 或 Ubuntu 24.04"
        "Linux|0|systemd|x86_64|alpine|3.23|僅支援 Debian 12/13 或 Ubuntu 24.04"
    )

    for row in "${cases[@]}"; do
        IFS='|' read -r kernel uid init arch distribution version expected <<< "$row"
        write_os_release "$distribution" "$version"
        run /usr/bin/env \
            S12RYT_KERNEL_NAME="$kernel" \
            S12RYT_EFFECTIVE_UID="$uid" \
            S12RYT_INIT_SYSTEM="$init" \
            S12RYT_MACHINE_ARCH="$arch" \
            S12RYT_OS_RELEASE_FILE="$S12RYT_OS_RELEASE_FILE" \
            /bin/bash -c 'source "$1"; check_ipv6_project_preflight' _ \
            "${PROJECT_ROOT}/install-ipv6.sh"

        [ "$status" -ne 0 ]
        [[ "$output" == *"$expected"* ]]
    done

    for row in 'debian|12' 'debian|13' 'ubuntu|24.04'; do
        IFS='|' read -r distribution version <<< "$row"
        write_os_release "$distribution" "$version"
        run /usr/bin/env \
            S12RYT_KERNEL_NAME=Linux \
            S12RYT_EFFECTIVE_UID=0 \
            S12RYT_INIT_SYSTEM=systemd \
            S12RYT_MACHINE_ARCH=aarch64 \
            S12RYT_OS_RELEASE_FILE="$S12RYT_OS_RELEASE_FILE" \
            /bin/bash -c 'source "$1"; check_ipv6_project_preflight' _ \
            "${PROJECT_ROOT}/install-ipv6.sh"

        [ "$status" -eq 0 ]
    done
}

@test "不支援的平台會在下載與舊版遷移前中止" {
    create_curl_mock
    create_systemctl_mock
    create_legacy_installation
    write_os_release ubuntu 22.04

    run_helper install

    [ "$status" -ne 0 ]
    [[ "$output" == *"僅支援 Debian 12/13 或 Ubuntu 24.04"* ]]
    [ ! -s "$MOCK_LOG" ]
    [ -s "${S12RYT_LEGACY_ROOT}/sentinel" ]
}

@test "安裝會綁定最新正式 tag 下載同版腳本並沿用預設管理埠" {
    create_curl_mock
    create_systemctl_mock

    run_helper install

    [ "$status" -eq 0 ]
    grep -Fq "curl -fsSL --connect-timeout 5 --max-time 60 ${S12RYT_RELEASE_API_URL}" "$MOCK_LOG"
    grep -Fq "${S12RYT_RAW_BASE_URL}/v0.1.8/install.sh" "$MOCK_LOG"
    grep -Fxq 'upstream-install VERSION=v0.1.8 MANAGEMENT_PORT=<unset>' "$MOCK_LOG"
}

@test "明確設定 MANAGEMENT_PORT 時原樣傳給上游安裝器" {
    create_curl_mock
    create_systemctl_mock

    run_helper install MANAGEMENT_PORT=35555

    [ "$status" -eq 0 ]
    grep -Fxq 'upstream-install VERSION=v0.1.8 MANAGEMENT_PORT=35555' "$MOCK_LOG"
}

@test "更新與安裝共用上游 install.sh 且綁定相同 VERSION" {
    create_curl_mock
    create_systemctl_mock

    run_helper update

    [ "$status" -eq 0 ]
    grep -Fq "${S12RYT_RAW_BASE_URL}/v0.1.8/install.sh" "$MOCK_LOG"
    grep -Fxq 'upstream-install VERSION=v0.1.8 MANAGEMENT_PORT=<unset>' "$MOCK_LOG"
}

@test "草稿預發布無效 tag 與畸形 metadata 均拒絕執行上游腳本" {
    create_curl_mock
    create_systemctl_mock
    local mode

    for mode in draft prerelease invalid-tag malformed; do
        : > "$MOCK_LOG"
        run_helper install "S12RYT_RELEASE_MODE=${mode}"

        [ "$status" -ne 0 ]
        [[ "$output" == *"GitHub Release metadata 無效"* ]]
        ! grep -Fq '/install.sh' "$MOCK_LOG"
        ! grep -Fq 'upstream-install' "$MOCK_LOG"
    done
}

@test "下載的上游腳本未通過 Bash 語法檢查時不執行" {
    create_curl_mock
    create_systemctl_mock

    run_helper install S12RYT_SCRIPT_MODE=invalid-syntax

    [ "$status" -ne 0 ]
    [[ "$output" == *"上游腳本語法驗證失敗"* ]]
    ! grep -Fq 'upstream-install' "$MOCK_LOG"
}

@test "舊版遷移先清理系統狀態再移除服務且完整保留 opt 資料" {
    create_curl_mock
    create_systemctl_mock
    create_legacy_installation

    run_helper install

    [ "$status" -eq 0 ]
    [ -s "${S12RYT_LEGACY_ROOT}/sentinel" ]
    [ -x "${S12RYT_LEGACY_ROOT}/bin/s12ryt-ipv6" ]
    [ ! -e "$S12RYT_SYSTEMD_UNIT_PATH" ]
    [ ! -e "$S12RYT_SYSTEMD_NETWORK_UNIT_PATH" ]
    [ "$(grep -n -m1 'legacy-panel cleanup-system' "$MOCK_LOG" | cut -d: -f1)" -lt \
        "$(grep -n -m1 'systemctl disable --now s12ryt-ipv6.service' "$MOCK_LOG" | cut -d: -f1)" ]
    grep -Fxq 'systemctl disable --now s12ryt-ipv6-network.service' "$MOCK_LOG"
    grep -Fxq 'systemctl daemon-reload' "$MOCK_LOG"
    grep -Fxq 'upstream-install VERSION=v0.1.8 MANAGEMENT_PORT=<unset>' "$MOCK_LOG"
}

@test "舊版清理失敗會中止且不下載新版" {
    create_curl_mock
    create_systemctl_mock
    create_legacy_installation

    run_helper install S12RYT_LEGACY_CLEANUP_FAIL=1

    [ "$status" -ne 0 ]
    [[ "$output" == *"舊版系統整合狀態清理失敗"* ]]
    [ -s "$S12RYT_SYSTEMD_UNIT_PATH" ]
    [ -s "$S12RYT_SYSTEMD_NETWORK_UNIT_PATH" ]
    ! grep -Fq '^curl ' "$MOCK_LOG"
    ! grep -Fq 'upstream-install' "$MOCK_LOG"
}

@test "舊服務移除失敗會盡力恢復原啟用與運行狀態並中止" {
    create_curl_mock
    create_systemctl_mock
    create_legacy_installation

    run_helper update S12RYT_SYSTEMCTL_FAIL=disable-network

    [ "$status" -ne 0 ]
    [[ "$output" == *"舊版服務移除失敗"* ]]
    grep -Fxq 'systemctl enable s12ryt-ipv6.service' "$MOCK_LOG"
    grep -Fxq 'systemctl start s12ryt-ipv6.service' "$MOCK_LOG"
    grep -Fxq 'systemctl enable s12ryt-ipv6-network.service' "$MOCK_LOG"
    grep -Fxq 'systemctl start s12ryt-ipv6-network.service' "$MOCK_LOG"
    ! grep -Fq '^curl ' "$MOCK_LOG"
    ! grep -Fq 'upstream-install' "$MOCK_LOG"
    [ -s "${S12RYT_LEGACY_ROOT}/sentinel" ]
}

@test "舊服務單元備份失敗會恢復服務並在下載新版前中止" {
    create_curl_mock
    create_systemctl_mock
    create_legacy_installation
    create_legacy_file_operation_mocks

    run_helper install S12RYT_FILE_OP_FAIL=backup-main

    [ "$status" -ne 0 ]
    [[ "$output" == *"舊版服務備份失敗"* ]]
    [ -s "$S12RYT_SYSTEMD_UNIT_PATH" ]
    [ -s "$S12RYT_SYSTEMD_NETWORK_UNIT_PATH" ]
    grep -Fxq 'systemctl enable s12ryt-ipv6.service' "$MOCK_LOG"
    grep -Fxq 'systemctl start s12ryt-ipv6-network.service' "$MOCK_LOG"
    ! grep -Fq '^curl ' "$MOCK_LOG"
    ! grep -Fq 'upstream-install' "$MOCK_LOG"
}

@test "舊服務單元刪除失敗會恢復服務並在下載新版前中止" {
    create_curl_mock
    create_systemctl_mock
    create_legacy_installation
    create_legacy_file_operation_mocks

    run_helper update S12RYT_FILE_OP_FAIL=remove-units

    [ "$status" -ne 0 ]
    [[ "$output" == *"舊版服務移除失敗"* ]]
    [ -s "$S12RYT_SYSTEMD_UNIT_PATH" ]
    [ -s "$S12RYT_SYSTEMD_NETWORK_UNIT_PATH" ]
    grep -Fxq 'systemctl enable s12ryt-ipv6-network.service' "$MOCK_LOG"
    grep -Fxq 'systemctl start s12ryt-ipv6.service' "$MOCK_LOG"
    ! grep -Fq '^curl ' "$MOCK_LOG"
    ! grep -Fq 'upstream-install' "$MOCK_LOG"
}

@test "只有新版同名主服務時不會誤判為舊版部署" {
    create_curl_mock
    create_systemctl_mock
    mkdir -p "${S12RYT_SYSTEMD_UNIT_PATH%/*}"
    printf 'new-main-unit\n' > "$S12RYT_SYSTEMD_UNIT_PATH"

    run_helper install

    [ "$status" -eq 0 ]
    [ -s "$S12RYT_SYSTEMD_UNIT_PATH" ]
    ! grep -Fq 'legacy-panel cleanup-system' "$MOCK_LOG"
    ! grep -Fq 'systemctl disable --now' "$MOCK_LOG"
    grep -Fxq 'upstream-install VERSION=v0.1.8 MANAGEMENT_PORT=<unset>' "$MOCK_LOG"
}

@test "卸載會執行同版上游 deploy uninstall 並保留資料目錄" {
    create_curl_mock
    create_systemctl_mock
    mkdir -p "$S12RYT_NEW_DATA_DIR"
    printf 'preserved-config\n' > "${S12RYT_NEW_DATA_DIR}/config.yaml"

    run_helper uninstall

    [ "$status" -eq 0 ]
    grep -Fq "${S12RYT_RAW_BASE_URL}/v0.1.8/deploy/uninstall.sh" "$MOCK_LOG"
    grep -Fxq 'upstream-uninstall VERSION=v0.1.8' "$MOCK_LOG"
    [ "$(cat "${S12RYT_NEW_DATA_DIR}/config.yaml")" = 'preserved-config' ]
    ! grep -Fq 'legacy-panel cleanup-system' "$MOCK_LOG"
}
