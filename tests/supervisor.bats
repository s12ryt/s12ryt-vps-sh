#!/usr/bin/env bats

load test_helper

setup() {
    setup_sandbox
    setup_mock_bin
    export CAPTURED_SERVICE_SCRIPT="${TEST_ROOT}/s12-service"
    create_supervisor_proot_mock
}

create_supervisor_proot_mock() {
    cat > "${MOCK_BIN}/proot-distro" <<'EOF'
#!/bin/bash
printf 'proot-distro' >> "$MOCK_LOG"
printf ' %s' "$@" >> "$MOCK_LOG"
printf '\n' >> "$MOCK_LOG"
if [[ "${1:-}" == "list" && "${2:-}" == "--quiet" ]]; then
    printf '%s\n' s12-debian13 s12-alpine323
elif [[ "${1:-}" == "copy" ]]; then
    cp "$2" "$CAPTURED_SERVICE_SCRIPT"
    chmod +x "$CAPTURED_SERVICE_SCRIPT"
fi
EOF
    chmod +x "${MOCK_BIN}/proot-distro"
}

run_supervisor_function() {
    run env \
        HOME="$HOME" \
        XDG_DATA_HOME="$XDG_DATA_HOME" \
        XDG_CACHE_HOME="$XDG_CACHE_HOME" \
        PATH="$MOCK_BIN:/usr/bin:/bin" \
        MOCK_LOG="$MOCK_LOG" \
        CAPTURED_SERVICE_SCRIPT="$CAPTURED_SERVICE_SCRIPT" \
        S12RYT_PROOT_DISTRO_BIN="$MOCK_BIN/proot-distro" \
        S12RYT_PROOT_SOURCE_ONLY=1 \
        /bin/bash -c 'source "$1"; shift; "$@"' _ \
        "${PROJECT_ROOT}/install-proot.sh" "$@"
}

@test "Supervisor 只能從已安裝的 PRoot 客體中選擇" {
    run env HOME="$HOME" XDG_DATA_HOME="$XDG_DATA_HOME" \
        XDG_CACHE_HOME="$XDG_CACHE_HOME" PATH="$MOCK_BIN:/usr/bin:/bin" \
        MOCK_LOG="$MOCK_LOG" S12RYT_PROOT_DISTRO_BIN="$MOCK_BIN/proot-distro" \
        S12RYT_PROOT_SOURCE_ONLY=1 /bin/bash -c \
        'source "$1"; printf "2\n" | select_installed_guest' \
        _ "${PROJECT_ROOT}/install-proot.sh"

    [ "$status" -eq 0 ]
    [[ "$output" == *"s12-debian13"* ]]
    [[ "$output" == *"s12-alpine323"* ]]
    [[ "$output" == *"已選擇: s12-alpine323"* ]]
    [[ "$output" != *"s12-ubuntu2604"* ]]
}

@test "Supervisor 拒絕未安裝或不受支援的客體名稱" {
    run_supervisor_function validate_installed_guest 's12-debian13;touch-pwned'

    [ "$status" -ne 0 ]
    [[ "$output" == *"客體尚未安裝或名稱不受支援"* ]]
    [ ! -e "${PROJECT_ROOT}/touch-pwned" ]
}

@test "Supervisor 依客體套件管理器安裝並傳入 s12-service" {
    run_supervisor_function install_supervisor_in_guest s12-debian13
    [ "$status" -eq 0 ]
    run_supervisor_function install_supervisor_in_guest s12-alpine323
    [ "$status" -eq 0 ]

    grep -Fq 'proot-distro login s12-debian13 -- sh -c apt-get update && DEBIAN_FRONTEND=noninteractive apt-get install -y supervisor' "$MOCK_LOG"
    grep -Fq 'proot-distro login s12-alpine323 -- sh -c apk add --no-cache supervisor' "$MOCK_LOG"
    grep -Fq 'proot-distro copy ' "$MOCK_LOG"
    grep -Fq 's12-debian13:/usr/local/bin/s12-service' "$MOCK_LOG"
    grep -Fq 's12-alpine323:/usr/local/bin/s12-service' "$MOCK_LOG"
    [ -x "$CAPTURED_SERVICE_SCRIPT" ]
    ! grep -Fq 'systemctl' "$CAPTURED_SERVICE_SCRIPT"
}

@test "s12-service 提供七個動作並安全切換 Supervisor 設定" {
    run_supervisor_function install_supervisor_in_guest s12-debian13
    [ "$status" -eq 0 ]

    cat > "${MOCK_BIN}/supervisorctl" <<'EOF'
#!/bin/bash
printf 'supervisorctl %s\n' "$*" >> "$MOCK_LOG"
EOF
    chmod +x "${MOCK_BIN}/supervisorctl"
    conf_dir="${TEST_ROOT}/supervisor-conf"
    mkdir -p "$conf_dir"
    : > "${conf_dir}/api.conf"

    for action in start stop restart status log; do
        run env PATH="$MOCK_BIN:/usr/bin:/bin" MOCK_LOG="$MOCK_LOG" \
            S12_SERVICE_CONF_DIR="$conf_dir" /bin/bash \
            "$CAPTURED_SERVICE_SCRIPT" "$action" api
        [ "$status" -eq 0 ]
    done
    run env PATH="$MOCK_BIN:/usr/bin:/bin" MOCK_LOG="$MOCK_LOG" \
        S12_SERVICE_CONF_DIR="$conf_dir" /bin/bash \
        "$CAPTURED_SERVICE_SCRIPT" disable api
    [ "$status" -eq 0 ]
    [ -f "${conf_dir}/api.conf.disabled" ]
    run env PATH="$MOCK_BIN:/usr/bin:/bin" MOCK_LOG="$MOCK_LOG" \
        S12_SERVICE_CONF_DIR="$conf_dir" /bin/bash \
        "$CAPTURED_SERVICE_SCRIPT" enable api
    [ "$status" -eq 0 ]
    [ -f "${conf_dir}/api.conf" ]

    grep -Fq 'supervisorctl start api' "$MOCK_LOG"
    grep -Fq 'supervisorctl stop api' "$MOCK_LOG"
    grep -Fq 'supervisorctl restart api' "$MOCK_LOG"
    grep -Fq 'supervisorctl status api' "$MOCK_LOG"
    grep -Fq 'supervisorctl tail -f api' "$MOCK_LOG"
    grep -Fq 'supervisorctl reread' "$MOCK_LOG"
    grep -Fq 'supervisorctl update' "$MOCK_LOG"

    : > "$MOCK_LOG"
    run env PATH="$MOCK_BIN:/usr/bin:/bin" MOCK_LOG="$MOCK_LOG" \
        S12_SERVICE_CONF_DIR="$conf_dir" /bin/bash \
        "$CAPTURED_SERVICE_SCRIPT" start '../bad'
    [ "$status" -ne 0 ]
    [[ "$output" == *"無效的服務名稱"* ]]
    [ ! -s "$MOCK_LOG" ]
}

@test "Supervisor 工作階段使用客體設定路徑並以 PRoot detached 模式啟動" {
    run_supervisor_function start_supervisor_session s12-debian13
    [ "$status" -eq 0 ]
    run_supervisor_function start_supervisor_session s12-alpine323
    [ "$status" -eq 0 ]

    grep -Fq 'proot-distro login s12-debian13 --detach -- supervisord -n -c /etc/supervisor/supervisord.conf' "$MOCK_LOG"
    grep -Fq 'proot-distro login s12-alpine323 --detach -- supervisord -n -c /etc/supervisord.conf' "$MOCK_LOG"
    [[ "$output" == *"只在此 PRoot Supervisor 工作階段存活"* ]]
    [[ "$output" == *"不是真正 systemd"* ]]
}

@test "主選單 6 會開啟 Supervisor 服務管理" {
    run env HOME="$HOME" S12RYT_SOURCE_ONLY=1 /bin/bash -c '
        source "$1"
        install_launcher() { :; }
        run_supervisor_manager() { printf "SUPERVISOR_MANAGER\n"; }
        printf "6\n0\n" | main
    ' _ "${PROJECT_ROOT}/s12ryt.sh"

    [ "$status" -eq 0 ]
    [[ "$output" == *"SUPERVISOR_MANAGER"* ]]
}
