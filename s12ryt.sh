#!/usr/bin/env bash

set -u

readonly VERSION="1.0.0"

read_os_name() {
    local os_release_file="${S12RYT_OS_RELEASE_FILE:-/etc/os-release}"
    local key value fallback="未知"

    [[ -r "$os_release_file" ]] || {
        printf '%s\n' "$fallback"
        return
    }
    while IFS='=' read -r key value; do
        value="${value%\"}"
        value="${value#\"}"
        case "$key" in
            PRETTY_NAME)
                printf '%s\n' "$value"
                return
                ;;
            NAME)
                fallback="$value"
                ;;
        esac
    done < "$os_release_file"
    printf '%s\n' "$fallback"
}

read_cpu_name() {
    local cpuinfo_file="${S12RYT_PROC_CPUINFO:-/proc/cpuinfo}"
    local cpu_name

    cpu_name="$(awk -F: '/^(model name|Hardware|Processor)[[:space:]]*:/ {
        sub(/^[[:space:]]+/, "", $2); print $2; exit
    }' "$cpuinfo_file" 2>/dev/null)"
    printf '%s\n' "${cpu_name:-未知}"
}

read_memory_usage() {
    local meminfo_file="${S12RYT_PROC_MEMINFO:-/proc/meminfo}"

    awk '
        /^MemTotal:/ { total = $2 }
        /^MemAvailable:/ { available = $2 }
        END {
            if (total > 0) {
                printf "%.2f GiB / %.2f GiB", (total - available) / 1048576, total / 1048576
            } else {
                printf "未知"
            }
        }
    ' "$meminfo_file" 2>/dev/null
}

read_root_disk_usage() {
    df -hP / 2>/dev/null | awk 'NR == 2 { printf "%s / %s (%s)", $3, $2, $5 }'
}

read_load_average() {
    local loadavg_file="${S12RYT_PROC_LOADAVG:-/proc/loadavg}"
    local one five fifteen _rest

    if read -r one five fifteen _rest < "$loadavg_file"; then
        printf '%s %s %s\n' "$one" "$five" "$fifteen"
    else
        printf '未知\n'
    fi
}

read_uptime() {
    local uptime_file="${S12RYT_PROC_UPTIME:-/proc/uptime}"
    local uptime_seconds _rest days hours minutes

    if ! read -r uptime_seconds _rest < "$uptime_file"; then
        printf '未知\n'
        return
    fi
    uptime_seconds="${uptime_seconds%%.*}"
    days=$((uptime_seconds / 86400))
    hours=$(((uptime_seconds % 86400) / 3600))
    minutes=$(((uptime_seconds % 3600) / 60))
    printf '%s 天 %s 小時 %s 分鐘\n' "$days" "$hours" "$minutes"
}

show_network_totals() {
    local net_dev_file="${S12RYT_PROC_NET_DEV:-/proc/net/dev}"
    local interface data receive _p1 _p2 _p3 _p4 _p5 _p6 _p7 transmit _rest

    printf '網路流量（開機累計）：\n'
    while IFS=: read -r interface data; do
        [[ -n "$data" ]] || continue
        interface="${interface//[[:space:]]/}"
        read -r receive _p1 _p2 _p3 _p4 _p5 _p6 _p7 transmit _rest <<< "$data"
        printf '  %s 接收: %s B 傳送: %s B\n' "$interface" "$receive" "$transmit"
    done < "$net_dev_file"
}

show_system_info() {
    local disk_usage

    disk_usage="$(read_root_disk_usage)"
    printf '作業系統: %s\n' "$(read_os_name)"
    printf '核心: %s\n' "$(uname -r)"
    printf '架構: %s\n' "$(uname -m)"
    printf 'CPU: %s\n' "$(read_cpu_name)"
    printf '記憶體: %s\n' "$(read_memory_usage)"
    printf '根目錄磁碟: %s\n' "${disk_usage:-未知}"
    printf '負載: %s\n' "$(read_load_average)"
    printf '運行時間: %s' "$(read_uptime)"
    printf '\n'
    show_network_totals
}

detect_package_manager() {
    local manager

    for manager in apt-get dnf yum apk pacman zypper; do
        if command -v "$manager" >/dev/null 2>&1; then
            printf '%s\n' "$manager"
            return 0
        fi
    done
    return 1
}

update_system() {
    local confirmation manager
    local -a privilege=()

    printf '即將更新套件索引並執行一般升級，是否繼續？ [y/N]: '
    IFS= read -r confirmation || confirmation=""
    case "$confirmation" in
        y|Y|yes|YES)
            ;;
        *)
            printf '已取消系統更新。\n'
            return 0
            ;;
    esac

    if ! manager="$(detect_package_manager)"; then
        printf '錯誤：找不到支援的套件管理器。\n' >&2
        return 1
    fi

    if (( EUID != 0 )); then
        if ! command -v sudo >/dev/null 2>&1 || ! sudo -n true >/dev/null 2>&1; then
            printf '錯誤：系統更新需要 root 權限或可用的 sudo。\n' >&2
            return 1
        fi
        privilege=(sudo)
    fi

    case "$manager" in
        apt-get)
            "${privilege[@]}" apt-get update && "${privilege[@]}" apt-get upgrade -y
            ;;
        dnf)
            "${privilege[@]}" dnf upgrade --refresh -y
            ;;
        yum)
            "${privilege[@]}" yum makecache && "${privilege[@]}" yum update -y
            ;;
        apk)
            "${privilege[@]}" apk update && "${privilege[@]}" apk upgrade
            ;;
        pacman)
            "${privilege[@]}" pacman -Syu --noconfirm
            ;;
        zypper)
            "${privilege[@]}" zypper --non-interactive refresh && \
                "${privilege[@]}" zypper --non-interactive update
            ;;
    esac || {
        printf '錯誤：系統更新失敗。\n' >&2
        return 1
    }

    printf '系統更新完成。\n'
}

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
        printf '%s\n' "export PATH=\"\$HOME/.local/bin:\$PATH\""
    fi
}

print_menu() {
    cat <<EOF
-----
s12ryt 的 VPS 腳本
版本: v${VERSION}
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
            1)
                show_system_info
                ;;
            2)
                update_system || true
                ;;
            3|4|5|6|7|8|9)
                not_implemented
                ;;
            *)
                printf '無效選項，請重新輸入。\n' >&2
                ;;
        esac
    done
}

if [[ "${BASH_SOURCE[0]}" == "$0" && "${S12RYT_SOURCE_ONLY:-0}" != "1" ]]; then
    main "$@"
fi
