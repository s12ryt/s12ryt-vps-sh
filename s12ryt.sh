#!/usr/bin/env bash

set -u

readonly VERSION="1.0.0"

script_path() {
    local source_path="${BASH_SOURCE[0]}"

    if command -v readlink >/dev/null 2>&1; then
        readlink -f "$source_path" 2>/dev/null && return 0
    fi

    printf '%s/%s\n' "$(cd "$(dirname "$source_path")" && pwd)" "$(basename "$source_path")"
}

install_launcher() {
    local source_path stable_path launcher_path launcher_dir stable_dir temp_launcher

    source_path="$(script_path)"
    if (( EUID == 0 )); then
        stable_path="/usr/local/lib/s12ryt/s12ryt.sh"
        launcher_path="/usr/local/bin/s"
    else
        stable_path="${HOME}/.local/share/s12ryt/s12ryt.sh"
        launcher_path="${HOME}/.local/bin/s"
    fi

    stable_dir="$(dirname "$stable_path")"
    launcher_dir="$(dirname "$launcher_path")"
    if ! mkdir -p "$stable_dir" "$launcher_dir"; then
        printf '錯誤：無法建立 s12ryt 安裝目錄。\n' >&2
        return 1
    fi

    if [[ "$source_path" != "$stable_path" ]]; then
        if ! cp "$source_path" "$stable_path" || ! chmod 0755 "$stable_path"; then
            printf '錯誤：無法建立 s12ryt 穩定副本。\n' >&2
            return 1
        fi
    fi

    temp_launcher="$(mktemp "${launcher_dir}/.s.XXXXXX")" || {
        printf '錯誤：無法建立 s 命令暫存檔。\n' >&2
        return 1
    }
    {
        printf '#!/usr/bin/env bash\n'
        printf 'exec %q "$@"\n' "$stable_path"
    } > "$temp_launcher"
    chmod 0755 "$temp_launcher"
    mv -f "$temp_launcher" "$launcher_path"

    if (( EUID != 0 )) && [[ ":${PATH}:" != *":${launcher_dir}:"* ]]; then
        printf '提示：請執行以下命令，讓 s 可直接使用：\n'
        printf '%s\n' 'export PATH="$HOME/.local/bin:$PATH"'
    fi
}

print_menu() {
    cat <<'EOF'
-----
s12ryt 的 VPS 腳本
版本: v1.0.0
-----
1. 系統資訊
2. 更新系統
3. IP 資訊
4. 自動 PRoot（腳本）
5. 自動 PRoot（安裝虛擬機）
6. 自動偽造 systemd
7. 自動安裝 Joey 的 fanout
8. s12ryt 項目列表
-----
9. 檢查更新
-----
0. 退出
-----
EOF
}

not_implemented() {
    printf '此功能將在後續版本完成。\n'
}

main() {
    local choice

    install_launcher || return 1
    while true; do
        print_menu
        printf '輸入選項: '
        if ! IFS= read -r choice; then
            printf '\n'
            return 0
        fi

        case "$choice" in
            0)
                printf '已退出。\n'
                return 0
                ;;
            1|2|3|4|5|6|7|8|9)
                not_implemented
                ;;
            *)
                printf '無效選項，請重新輸入。\n' >&2
                ;;
        esac
    done
}

main "$@"
