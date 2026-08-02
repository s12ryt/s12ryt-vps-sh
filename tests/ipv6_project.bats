#!/usr/bin/env bats

load test_helper

setup() {
    setup_sandbox
}

@test "項目列表顯示多 IPv6 出站並可進入後返回" {
    run env HOME="$HOME" S12RYT_SOURCE_ONLY=1 /bin/bash -c '
        source "$1"
        run_ipv6_project_menu() { printf "ipv6-project-menu-called\n"; }
        printf "invalid\n1\n0\n" | show_projects
    ' _ "${PROJECT_ROOT}/s12ryt.sh"

    [ "$status" -eq 0 ]
    [[ "$output" == *"s12ryt 項目列表"* ]]
    [[ "$output" == *"1. s12ryt-多ipv6出站"* ]]
    [[ "$output" == *"0. 返回"* ]]
    [[ "$output" == *"無效選項"* ]]
    [[ "$output" == *"ipv6-project-menu-called"* ]]
    [ "${output//s12ryt 項目列表/}" != "$output" ]
}

@test "多 IPv6 出站子選單接線安裝更新設定卸載" {
    run env HOME="$HOME" S12RYT_SOURCE_ONLY=1 /bin/bash -c '
        source "$1"
        install_ipv6_project() { printf "install-ipv6-called\n"; }
        update_ipv6_project() { printf "update-ipv6-called\n"; }
        configure_ipv6_project() { printf "configure-ipv6-called\n"; }
        uninstall_ipv6_project() { printf "uninstall-ipv6-called\n"; }
        printf "1\n2\n3\n4\ninvalid\n0\n" | run_ipv6_project_menu
    ' _ "${PROJECT_ROOT}/s12ryt.sh"

    [ "$status" -eq 0 ]
    [[ "$output" == *"s12ryt特供-多ipv6出站（web管理面板）"* ]]
    [[ "$output" == *"1. 安裝"* ]]
    [[ "$output" == *"2. 更新"* ]]
    [[ "$output" == *"3. 設定"* ]]
    [[ "$output" == *"4. 卸載"* ]]
    [[ "$output" == *"0. 退出"* ]]
    [[ "$output" == *"install-ipv6-called"* ]]
    [[ "$output" == *"update-ipv6-called"* ]]
    [[ "$output" == *"configure-ipv6-called"* ]]
    [[ "$output" == *"uninstall-ipv6-called"* ]]
    [[ "$output" == *"無效選項"* ]]
}
