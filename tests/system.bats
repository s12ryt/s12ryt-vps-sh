#!/usr/bin/env bats

load test_helper

setup() {
    setup_sandbox
    setup_mock_bin
}

@test "系統資訊顯示契約欄位與各介面開機累計流量" {
    run env \
        HOME="$HOME" \
        S12RYT_SOURCE_ONLY=1 \
        S12RYT_OS_RELEASE_FILE="${PROJECT_ROOT}/tests/fixtures/os-release" \
        S12RYT_PROC_NET_DEV="${PROJECT_ROOT}/tests/fixtures/proc-net-dev" \
        /bin/bash -c 'source "$1"; show_system_info' _ "${PROJECT_ROOT}/s12ryt.sh"

    [ "$status" -eq 0 ]
    [[ "$output" == *"作業系統: Fixture Linux 1.0"* ]]
    [[ "$output" == *"核心:"* ]]
    [[ "$output" == *"架構:"* ]]
    [[ "$output" == *"CPU:"* ]]
    [[ "$output" == *"記憶體:"* ]]
    [[ "$output" == *"根目錄磁碟:"* ]]
    [[ "$output" == *"負載:"* ]]
    [[ "$output" == *"運行時間:"* ]]
    [[ "$output" == *"lo 接收: 1000 B 傳送: 2000 B"* ]]
    [[ "$output" == *"eth0 接收: 123456789 B 傳送: 987654321 B"* ]]
}

@test "取消更新時不執行任何套件命令" {
    create_command_mock apt-get

    run env HOME="$HOME" PATH="$MOCK_BIN" MOCK_LOG="$MOCK_LOG" S12RYT_SOURCE_ONLY=1 \
        /bin/bash -c 'source "$1"; printf "n\n" | update_system' _ "${PROJECT_ROOT}/s12ryt.sh"

    [ "$status" -eq 0 ]
    [[ "$output" == *"已取消系統更新"* ]]
    [ ! -s "$MOCK_LOG" ]
}

@test "非 root 且沒有可用 sudo 時拒絕更新" {
    create_command_mock apt-get

    run env HOME="$HOME" PATH="$MOCK_BIN" MOCK_LOG="$MOCK_LOG" S12RYT_SOURCE_ONLY=1 \
        /bin/bash -c 'source "$1"; printf "y\n" | update_system' _ "${PROJECT_ROOT}/s12ryt.sh"

    [ "$status" -ne 0 ]
    [[ "$output" == *"需要 root 權限或可用的 sudo"* ]]
    [ ! -s "$MOCK_LOG" ]
}

@test "支援的套件管理器只執行一般更新與升級" {
    local manager expected

    for manager in apt-get dnf yum apk pacman zypper; do
        rm -f "${MOCK_BIN:?}"/*
        : > "$MOCK_LOG"
        create_command_mock sudo
        create_command_mock "$manager"

        run env HOME="$HOME" PATH="$MOCK_BIN" MOCK_LOG="$MOCK_LOG" S12RYT_SOURCE_ONLY=1 \
            /bin/bash -c 'source "$1"; printf "y\n" | update_system' _ "${PROJECT_ROOT}/s12ryt.sh"

        [ "$status" -eq 0 ]
        case "$manager" in
            apt-get) expected=$'sudo apt-get update\napt-get update\nsudo apt-get upgrade -y\napt-get upgrade -y' ;;
            dnf) expected=$'sudo dnf upgrade --refresh -y\ndnf upgrade --refresh -y' ;;
            yum) expected=$'sudo yum makecache\nyum makecache\nsudo yum update -y\nyum update -y' ;;
            apk) expected=$'sudo apk update\napk update\nsudo apk upgrade\napk upgrade' ;;
            pacman) expected=$'sudo pacman -Syu --noconfirm\npacman -Syu --noconfirm' ;;
            zypper) expected=$'sudo zypper --non-interactive refresh\nzypper --non-interactive refresh\nsudo zypper --non-interactive update\nzypper --non-interactive update' ;;
        esac
        [ "$(cat "$MOCK_LOG")" = "$expected" ]
        [[ "$(cat "$MOCK_LOG")" != *"full-upgrade"* ]]
        [[ "$output" == *"系統更新完成"* ]]
    done
}
