#!/usr/bin/env bats

load test_helper

setup() {
    setup_sandbox
    export TRACE_FILE="${TEST_ROOT}/menu-flow.log"
    : > "$TRACE_FILE"
}

@test "互動模式清除目前畫面與終端捲動歷史" {
    run /usr/bin/env S12RYT_SOURCE_ONLY=1 S12RYT_FORCE_INTERACTIVE=1 /bin/bash -c '
        source "$1"
        clear_terminal_history
    ' _ "${PROJECT_ROOT}/s12ryt.sh"

    [ "$status" -eq 0 ]
    [ "$output" = $'\033[2J\033[3J\033[H' ]
}

@test "非互動模式不輸出控制碼也不等待按鍵" {
    run /usr/bin/env S12RYT_SOURCE_ONLY=1 S12RYT_FORCE_INTERACTIVE=0 /bin/bash -c '
        source "$1"
        clear_terminal_history
        wait_for_return_key
        printf "finished\n"
    ' _ "${PROJECT_ROOT}/s12ryt.sh"

    [ "$status" -eq 0 ]
    [ "$output" = "finished" ]
}

@test "返回提示接受無換行的任意單鍵" {
    run /usr/bin/env S12RYT_SOURCE_ONLY=1 S12RYT_FORCE_INTERACTIVE=1 /bin/bash -c '
        source "$1"
        printf x | wait_for_return_key
    ' _ "${PROJECT_ROOT}/s12ryt.sh"

    [ "$status" -eq 0 ]
    [ "$output" = "按隨意鍵以返回腳本" ]
}

@test "成功取消與錯誤都依序清除暫停再清除" {
    run /usr/bin/env S12RYT_SOURCE_ONLY=1 TRACE_FILE="$TRACE_FILE" /bin/bash -c '
        source "$1"
        clear_terminal_history() { printf "clear\n" >> "$TRACE_FILE"; }
        wait_for_return_key() { printf "pause\n" >> "$TRACE_FILE"; }
        success_action() { printf "success\n" >> "$TRACE_FILE"; }
        cancel_action() { printf "cancel\n" >> "$TRACE_FILE"; return 2; }
        error_action() { printf "error\n" >> "$TRACE_FILE"; return 1; }

        for action in success_action cancel_action error_action; do
            action_status=0
            run_menu_action "$action" || action_status=$?
            printf "status:%s\n" "$action_status" >> "$TRACE_FILE"
        done
    ' _ "${PROJECT_ROOT}/s12ryt.sh"

    [ "$status" -eq 0 ]
    run cat "$TRACE_FILE"
    [ "$output" = $'clear\nsuccess\npause\nclear\nstatus:0\nclear\ncancel\npause\nclear\nstatus:2\nclear\nerror\npause\nclear\nstatus:1' ]
}

@test "主選單首次與功能前後清除但退出及無效輸入不暫停" {
    run /usr/bin/env HOME="$HOME" S12RYT_SOURCE_ONLY=1 TRACE_FILE="$TRACE_FILE" /bin/bash -c '
        source "$1"
        install_launcher() { :; }
        print_menu() { printf "menu\n" >> "$TRACE_FILE"; }
        clear_terminal_history() { printf "clear\n" >> "$TRACE_FILE"; }
        wait_for_return_key() { printf "pause\n" >> "$TRACE_FILE"; }
        show_projects() { printf "projects\n" >> "$TRACE_FILE"; }
        main <<EOF
8
invalid
0
EOF
    ' _ "${PROJECT_ROOT}/s12ryt.sh"

    [ "$status" -eq 0 ]
    run cat "$TRACE_FILE"
    [ "$output" = $'clear\nmenu\nclear\nprojects\npause\nclear\nmenu\nmenu' ]
}

@test "PRoot 與 Supervisor 各自只套用一次頂層返回流程" {
    run /usr/bin/env HOME="$HOME" S12RYT_SOURCE_ONLY=1 TRACE_FILE="$TRACE_FILE" /bin/bash -c '
        source "$1"
        install_launcher() { :; }
        print_menu() { printf "menu\n" >> "$TRACE_FILE"; }
        clear_terminal_history() { printf "clear\n" >> "$TRACE_FILE"; }
        wait_for_return_key() { printf "pause\n" >> "$TRACE_FILE"; }
        run_proot_manager() { printf "proot-submenu\n" >> "$TRACE_FILE"; }
        run_supervisor_manager() { printf "supervisor-submenu\n" >> "$TRACE_FILE"; }
        main <<EOF
5
6
0
EOF
    ' _ "${PROJECT_ROOT}/s12ryt.sh"

    [ "$status" -eq 0 ]
    run cat "$TRACE_FILE"
    [ "$output" = $'clear\nmenu\nclear\nproot-submenu\npause\nclear\nmenu\nclear\nsupervisor-submenu\npause\nclear\nmenu' ]
}

@test "主選單完全符合新版分隔與編號" {
    run /usr/bin/env S12RYT_SOURCE_ONLY=1 /bin/bash -c '
        source "$1"
        print_menu
    ' _ "${PROJECT_ROOT}/s12ryt.sh"

    [ "$status" -eq 0 ]
    [ "$output" = $'-----\ns12ryt 的 VPS 腳本\n版本: v1.1.1\nCopyright (C) 2026 s12ryt\n授權: GPL-3.0-only；本程式不提供任何擔保，詳見 LICENSE。\n-----\n1. 系統資訊\n2. 更新系統\n3. IP 資訊\n4. 自動 PRoot（腳本）\n5. 自動 PRoot（安裝虛擬機）\n6. 自動偽造 systemd\n7. 自動安裝 Joey 的 fanout\n8. s12ryt 項目列表\n9. 安裝 Python\n10. 安裝 Node.js\n-----\n11. 檢查更新\n-----\n0. 退出\n-----' ]
}

@test "腳本不得清除 Bash 指令歷史" {
    run grep -E 'history[[:space:]]+-c|HISTFILE' "${PROJECT_ROOT}/s12ryt.sh"

    [ "$status" -ne 0 ]
}
