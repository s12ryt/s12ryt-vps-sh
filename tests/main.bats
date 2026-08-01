#!/usr/bin/env bats

load test_helper

setup() {
    setup_sandbox
}

@test "啟動時顯示版本與完整主選單並可正常退出" {
    run_menu $'0\n'

    [ "$status" -eq 0 ]
    [[ "$output" == *"s12ryt 的 VPS 腳本"* ]]
    [[ "$output" == *"版本: v1.0.0"* ]]
    [[ "$output" == *"Copyright (C) 2026 s12ryt"* ]]
    [[ "$output" == *"GPL-3.0-only"* ]]
    [[ "$output" == *"不提供任何擔保"* ]]
    [[ "$output" == *"1. 系統資訊"* ]]
    [[ "$output" == *"2. 更新系統"* ]]
    [[ "$output" == *"3. IP 資訊"* ]]
    [[ "$output" == *"4. 自動 PRoot（腳本）"* ]]
    [[ "$output" == *"5. 自動 PRoot（安裝虛擬機）"* ]]
    [[ "$output" == *"6. 自動偽造 systemd"* ]]
    [[ "$output" == *"7. 自動安裝 Joey 的 fanout"* ]]
    [[ "$output" == *"8. s12ryt 項目列表"* ]]
    [[ "$output" == *"9. 檢查更新"* ]]
    [[ "$output" == *"0. 退出"* ]]
}

@test "無效輸入顯示錯誤後返回主選單" {
    run_menu $'invalid\n0\n'

    [ "$status" -eq 0 ]
    [[ "$output" == *"無效選項"* ]]
    [ "${output//輸入選項/}" != "$output" ]
}

@test "非 root 執行會建立穩定副本與可再次啟動的 s 命令" {
    run_menu $'0\n'

    [ "$status" -eq 0 ]
    [ -f "$HOME/.local/share/s12ryt/s12ryt.sh" ]
    [ -x "$HOME/.local/bin/s" ]

    run bash -c 'printf "0\n" | "$HOME/.local/bin/s"'
    [ "$status" -eq 0 ]
    [[ "$output" == *"版本: v1.0.0"* ]]
}

@test "安裝 s 命令時強制覆寫既有檔案且只提示 PATH" {
    mkdir -p "$HOME/.local/bin"
    printf '%s\n' '#!/usr/bin/env bash' 'printf sentinel' > "$HOME/.local/bin/s"
    chmod +x "$HOME/.local/bin/s"

    run_menu $'0\n'

    [ "$status" -eq 0 ]
    menu_output="$output"
    [[ "$menu_output" == *'export PATH="$HOME/.local/bin:$PATH"'* ]]
    run grep -F sentinel "$HOME/.local/bin/s"
    [ "$status" -ne 0 ]
    [[ "$output" != *"sentinel"* ]]

    [ ! -e "$HOME/.bashrc" ]
}
