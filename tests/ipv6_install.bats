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
    *:/s12ryt-ipv6-linux-amd64)
        printf 'panel-amd64\n' > "$output"
        ;;
    *:/sing-box-1.13.15-linux-amd64.tar.gz)
        if [[ "${S12RYT_CURL_MODE:-success}" == "archive-fail" ]]; then
            printf 'not-an-archive\n' > "$output"
        else
            archive_root="${output%/*}/archive-root"
            mkdir -p "${archive_root}/sing-box-1.13.15-linux-amd64"
            printf 'sing-box-amd64\n' > "${archive_root}/sing-box-1.13.15-linux-amd64/sing-box"
            /usr/bin/tar -czf "$output" -C "$archive_root" sing-box-1.13.15-linux-amd64
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
