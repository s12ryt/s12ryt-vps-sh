#!/usr/bin/env bats

load test_helper

setup() {
    setup_sandbox
    setup_mock_bin
    export TUN_PATH="${TEST_ROOT}/dev/net/tun"
    export FANOUT_EXEC_LOG="${TEST_ROOT}/fanout-executed.log"
    mkdir -p "${TUN_PATH%/*}"
}

create_unshare_mock() {
    local exit_code="${1:-0}"

    cat > "${MOCK_BIN}/unshare" <<EOF
#!/bin/bash
printf 'unshare %s\n' "\$*" >> "\$MOCK_LOG"
exit ${exit_code}
EOF
    chmod +x "${MOCK_BIN}/unshare"
}

create_fanout_curl_mock() {
    cat > "${MOCK_BIN}/curl" <<'EOF'
#!/bin/bash
printf 'curl %s\n' "$*" >> "$MOCK_LOG"
output=""
while (($#)); do
    if [[ "$1" == "-o" ]]; then
        output="$2"
        shift 2
    else
        shift
    fi
done
case "${FANOUT_CURL_MODE:-success}" in
    fail)
        exit 22
        ;;
    invalid)
        printf '%s\n' '#!/usr/bin/env bash' 'if broken' > "$output"
        ;;
    success)
        cat > "$output" <<'SCRIPT'
#!/usr/bin/env bash
printf 'executed\n' >> "$FANOUT_EXEC_LOG"
SCRIPT
        ;;
esac
EOF
    chmod +x "${MOCK_BIN}/curl"
}

run_fanout_function() {
    run env \
        PATH="$MOCK_BIN:/usr/bin:/bin" \
        MOCK_LOG="$MOCK_LOG" \
        FANOUT_EXEC_LOG="$FANOUT_EXEC_LOG" \
        S12RYT_EFFECTIVE_UID="${S12RYT_EFFECTIVE_UID:-0}" \
        S12RYT_KERNEL_NAME="${S12RYT_KERNEL_NAME:-Linux}" \
        S12RYT_TUN_PATH="$TUN_PATH" \
        S12RYT_INIT_SYSTEM="${S12RYT_INIT_SYSTEM:-systemd}" \
        S12RYT_SOURCE_ONLY=1 \
        /bin/bash -c 'source "$1"; shift; "$@"' _ \
        "${PROJECT_ROOT}/s12ryt.sh" "$@"
}

@test "Fanout 逐項拒絕不相容平台、權限、TUN、netns 與 init" {
    create_unshare_mock 0
    create_fanout_curl_mock
    touch "$TUN_PATH"

    S12RYT_KERNEL_NAME=Darwin run_fanout_function check_fanout_prerequisites
    [ "$status" -ne 0 ]
    [[ "$output" == *"只支援 Linux"* ]]

    S12RYT_EFFECTIVE_UID=1000 run_fanout_function check_fanout_prerequisites
    [ "$status" -ne 0 ]
    [[ "$output" == *"需要 root 權限"* ]]

    rm -f "$TUN_PATH"
    run_fanout_function check_fanout_prerequisites
    [ "$status" -ne 0 ]
    [[ "$output" == *"/dev/net/tun"* ]]

    touch "$TUN_PATH"
    create_unshare_mock 1
    run_fanout_function check_fanout_prerequisites
    [ "$status" -ne 0 ]
    [[ "$output" == *"network namespace"* ]]

    create_unshare_mock 0
    S12RYT_INIT_SYSTEM=none run_fanout_function check_fanout_prerequisites
    [ "$status" -ne 0 ]
    [[ "$output" == *"systemd 或 OpenRC"* ]]
    ! grep -q '^curl ' "$MOCK_LOG"
}

@test "Fanout 前置條件通過後下載官方腳本並不再確認直接執行" {
    create_unshare_mock 0
    create_fanout_curl_mock
    touch "$TUN_PATH"

    run_fanout_function install_fanout </dev/null

    [ "$status" -eq 0 ]
    [ -s "$FANOUT_EXEC_LOG" ]
    grep -Fq 'https://raw.githubusercontent.com/byJoey/fanout/main/install.sh' "$MOCK_LOG"
    grep -q -- '--connect-timeout' "$MOCK_LOG"
    grep -q -- '--max-time' "$MOCK_LOG"
    [[ "$output" != *"是否繼續"* ]]
}

@test "Fanout 下載或語法驗證失敗時不執行上游腳本" {
    create_unshare_mock 0
    create_fanout_curl_mock
    touch "$TUN_PATH"

    export FANOUT_CURL_MODE=fail
    run_fanout_function install_fanout
    [ "$status" -ne 0 ]
    [[ "$output" == *"無法下載 Fanout 安裝腳本"* ]]
    [ ! -e "$FANOUT_EXEC_LOG" ]

    export FANOUT_CURL_MODE=invalid
    run_fanout_function install_fanout
    [ "$status" -ne 0 ]
    [[ "$output" == *"Fanout 安裝腳本語法驗證失敗"* ]]
    [ ! -e "$FANOUT_EXEC_LOG" ]
}

@test "主選單 7 會執行 Fanout 安裝入口" {
    run env HOME="$HOME" S12RYT_SOURCE_ONLY=1 /bin/bash -c '
        source "$1"
        install_launcher() { :; }
        install_fanout() { printf "FANOUT_MARKER\n"; }
        printf "7\n0\n" | main
    ' _ "${PROJECT_ROOT}/s12ryt.sh"

    [ "$status" -eq 0 ]
    [[ "$output" == *"FANOUT_MARKER"* ]]
}
