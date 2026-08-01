#!/usr/bin/env bats

load test_helper

setup() {
    setup_sandbox
    setup_mock_bin
    setup_runtime_path
    export NODE_READY_FILE="${TEST_ROOT}/node-ready"
}

setup_runtime_path() {
    local command_path command_name

    for command_name in bash cat chmod mktemp rm touch; do
        command_path="$(command -v "$command_name")"
        ln -s "$command_path" "${MOCK_BIN}/${command_name}"
    done
    export PATH="$MOCK_BIN"
}

create_node_commands() {
    cat > "${MOCK_BIN}/node" <<'EOF'
#!/bin/bash
[[ -f "$NODE_READY_FILE" ]] || exit 1
printf 'v%s\n' "${S12RYT_TEST_NODE_VERSION:-24.7.0}"
EOF
    cat > "${MOCK_BIN}/npm" <<'EOF'
#!/bin/bash
[[ -f "$NODE_READY_FILE" ]] || exit 1
printf '11.5.1\n'
EOF
    chmod +x "${MOCK_BIN}/node" "${MOCK_BIN}/npm"
}

create_node_manager_mock() {
    local manager="$1"

    cat > "${MOCK_BIN}/${manager}" <<'EOF'
#!/bin/bash
printf '%s' "${0##*/}" >> "$MOCK_LOG"
printf ' %s' "$@" >> "$MOCK_LOG"
printf '\n' >> "$MOCK_LOG"
if [[ " $* " == *" install "* && " $* " == *" nodejs "* ]]; then
    : > "$NODE_READY_FILE"
fi
EOF
    chmod +x "${MOCK_BIN}/${manager}"
}

create_nodesource_curl_mock() {
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
case "${S12RYT_CURL_MODE:-success}" in
    download-fail)
        exit 22
        ;;
    syntax-fail)
        printf 'if broken\n' > "$output"
        ;;
    setup-fail)
        printf '%s\n' '#!/usr/bin/env bash' 'exit 7' > "$output"
        ;;
    *)
        cat > "$output" <<'SCRIPT'
#!/usr/bin/env bash
printf 'nodesource-setup-executed\n' >> "$MOCK_LOG"
SCRIPT
        ;;
esac
EOF
    chmod +x "${MOCK_BIN}/curl"
}

run_node_installer() {
    local input="$1"
    shift

    run /usr/bin/env HOME="$HOME" PATH="$PATH" MOCK_LOG="$MOCK_LOG" \
        NODE_READY_FILE="$NODE_READY_FILE" S12RYT_SOURCE_ONLY=1 \
        S12RYT_EFFECTIVE_UID="${S12RYT_EFFECTIVE_UID:-1000}" \
        S12RYT_MACHINE_ARCH="${S12RYT_MACHINE_ARCH:-x86_64}" \
        "$@" /bin/bash -c 'source "$1"; printf "%s" "$2" | install_nodejs' \
        _ "${PROJECT_ROOT}/s12ryt.sh" "$input"
}

@test "既有 Node.js 顯示版本並完全跳過安裝" {
    create_node_commands
    : > "$NODE_READY_FILE"

    run_node_installer ''

    [ "$status" -eq 0 ]
    [[ "$output" == *"v24.7.0"* ]]
    [[ "$output" == *"已安裝"* ]]
    [ ! -s "$MOCK_LOG" ]
}

@test "NodeSource 拒絕不支援的套件管理器與架構" {
    create_node_commands
    create_node_manager_mock apk

    run_node_installer ''
    [ "$status" -ne 0 ]
    [[ "$output" == *"NodeSource"* ]]
    [[ "$output" == *"不支援套件管理器 apk"* ]]

    rm -f "${MOCK_BIN}/apk"
    create_node_manager_mock apt-get
    S12RYT_MACHINE_ARCH=mips64 run_node_installer ''
    [ "$status" -ne 0 ]
    [[ "$output" == *"不支援架構 mips64"* ]]
}

@test "取消 Node.js 安裝時不下載或執行命令" {
    create_node_commands
    create_node_manager_mock apt-get
    create_nodesource_curl_mock

    run_node_installer $'n\n'

    [ "$status" -eq 0 ]
    [[ "$output" == *"套件管理器: apt-get"* ]]
    [[ "$output" == *"已取消 Node.js 安裝"* ]]
    [ ! -s "$MOCK_LOG" ]
}

@test "Node.js 20 顯示 EOL 警告並要求第二次確認" {
    create_node_commands
    create_node_manager_mock apt-get
    create_nodesource_curl_mock

    run_node_installer $'y\n20\nn\n'

    [ "$status" -eq 0 ]
    [[ "$output" == *"Node.js 20"* ]]
    [[ "$output" == *"EOL"* ]]
    [[ "$output" == *"2026-03-24"* ]]
    [ ! -s "$MOCK_LOG" ]
}

@test "NodeSource 20 22 24 使用正確來源與套件命令並驗證 node npm" {
    local manager major base_url input expected_version

    for row in 'apt-get 20 https://deb.nodesource.com' \
        'dnf 22 https://rpm.nodesource.com' \
        'yum 24 https://rpm.nodesource.com'; do
        read -r manager major base_url <<< "$row"
        rm -f "${MOCK_BIN}/apt-get" "${MOCK_BIN}/dnf" "${MOCK_BIN}/yum" \
            "$NODE_READY_FILE"
        : > "$MOCK_LOG"
        create_node_commands
        create_node_manager_mock "$manager"
        create_nodesource_curl_mock
        create_command_mock sudo
        expected_version="${major}.7.0"
        export S12RYT_TEST_NODE_VERSION="$expected_version"
        if [[ "$major" == "20" ]]; then
            input=$'y\n20\ny\n'
        else
            input="$(printf 'y\n%s\n' "$major")"
        fi

        run_node_installer "$input"

        [ "$status" -eq 0 ]
        [[ "$(cat "$MOCK_LOG")" == *"curl -fsSL --connect-timeout 5 --max-time 30 ${base_url}/setup_${major}.x"* ]]
        [[ "$(cat "$MOCK_LOG")" == *"sudo -n true"* ]]
        [[ "$(cat "$MOCK_LOG")" == *"sudo -n bash "* ]]
        [[ "$(cat "$MOCK_LOG")" == *"nodesource-setup-executed"* ]]
        [[ "$(cat "$MOCK_LOG")" == *"sudo -n ${manager} install -y nodejs"* ]]
        [[ "$output" == *"Node.js: v${expected_version}"* ]]
        [[ "$output" == *"npm: 11.5.1"* ]]
    done
}

@test "Node.js 無非互動管理權限時拒絕且不下載" {
    create_node_commands
    create_node_manager_mock apt-get
    create_nodesource_curl_mock

    run_node_installer $'y\n22\n'

    [ "$status" -ne 0 ]
    [[ "$output" == *"需要 root 權限或可用的 sudo"* ]]
    [ ! -s "$MOCK_LOG" ]
}

@test "NodeSource 下載語法或執行失敗時不安裝 nodejs" {
    local mode

    for mode in download-fail syntax-fail setup-fail; do
        rm -f "$NODE_READY_FILE"
        : > "$MOCK_LOG"
        create_node_commands
        create_node_manager_mock apt-get
        create_nodesource_curl_mock
        export S12RYT_EFFECTIVE_UID=0

        run_node_installer $'y\n22\n' S12RYT_CURL_MODE="$mode"

        [ "$status" -ne 0 ]
        [[ "$(cat "$MOCK_LOG")" != *"apt-get install -y nodejs"* ]]
        [ ! -e "$NODE_READY_FILE" ]
    done
}
