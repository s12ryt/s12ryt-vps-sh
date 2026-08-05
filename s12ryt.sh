#!/usr/bin/env bash
# Copyright (C) 2026 s12ryt
# SPDX-License-Identifier: GPL-3.0-only

set -u

readonly VERSION="1.1.1"

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
        privilege=(sudo -n)
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

install_nodejs() {
    local node_version npm_version manager machine_arch confirmation major installer_url temp_file
    local effective_uid="${S12RYT_EFFECTIVE_UID:-$EUID}"
    local -a privilege=()

    if node_version="$(node --version 2>/dev/null)"; then
        printf 'Node.js 已安裝：%s\n' "$node_version"
        return 0
    fi

    if ! manager="$(detect_package_manager)"; then
        printf '錯誤：找不到 NodeSource 支援的套件管理器。\n' >&2
        return 1
    fi
    case "$manager" in
        apt-get)
            installer_url='https://deb.nodesource.com'
            ;;
        dnf|yum)
            installer_url='https://rpm.nodesource.com'
            ;;
        *)
            printf '錯誤：NodeSource 不支援套件管理器 %s。\n' "$manager" >&2
            return 1
            ;;
    esac

    machine_arch="${S12RYT_MACHINE_ARCH:-$(uname -m)}"
    case "$machine_arch" in
        x86_64|amd64|aarch64|arm64)
            ;;
        *)
            printf '錯誤：NodeSource 不支援架構 %s。\n' "$machine_arch" >&2
            return 1
            ;;
    esac

    printf '套件管理器: %s\n' "$manager"
    printf '是否使用 NodeSource 安裝 Node.js？ [y/N]: '
    confirmation=""
    IFS= read -r confirmation || true
    case "$confirmation" in
        y|Y|yes|YES)
            ;;
        *)
            printf '已取消 Node.js 安裝。\n'
            return 0
            ;;
    esac

    printf '可選版本：20、22、24（26 目前不是 LTS）\n'
    printf '請輸入 Node.js major: '
    major=""
    IFS= read -r major || true
    case "$major" in
        20|22|24)
            ;;
        *)
            printf '錯誤：只支援 Node.js 20、22 或 24。\n' >&2
            return 1
            ;;
    esac
    if [[ "$major" == "20" ]]; then
        printf '警告：Node.js 20 已於 2026-03-24 EOL，不再接受官方安全維護。\n'
        printf '仍要安裝 Node.js 20？ [y/N]: '
        confirmation=""
        IFS= read -r confirmation || true
        case "$confirmation" in
            y|Y|yes|YES)
                ;;
            *)
                printf '已取消 Node.js 20 安裝。\n'
                return 0
                ;;
        esac
    fi

    if [[ ! "$effective_uid" =~ ^[0-9]+$ ]]; then
        printf '錯誤：無法判斷目前管理權限。\n' >&2
        return 1
    fi
    if (( effective_uid != 0 )); then
        if ! command -v sudo >/dev/null 2>&1 || ! sudo -n true >/dev/null 2>&1; then
            printf '錯誤：Node.js 安裝需要 root 權限或可用的 sudo。\n' >&2
            return 1
        fi
        privilege=(sudo -n)
    fi
    if ! command -v curl >/dev/null 2>&1; then
        printf '錯誤：下載 NodeSource 安裝腳本需要 curl。\n' >&2
        return 1
    fi

    temp_file="$(mktemp "${TMPDIR:-/tmp}/s12ryt-nodesource.XXXXXX")" || {
        printf '錯誤：無法建立 NodeSource 暫存檔。\n' >&2
        return 1
    }
    installer_url="${installer_url}/setup_${major}.x"
    if ! curl -fsSL --connect-timeout 5 --max-time 30 "$installer_url" -o "$temp_file"; then
        rm -f "$temp_file"
        printf '錯誤：無法下載 NodeSource 安裝腳本。\n' >&2
        return 1
    fi
    if ! bash -n "$temp_file"; then
        rm -f "$temp_file"
        printf '錯誤：NodeSource 安裝腳本語法驗證失敗。\n' >&2
        return 1
    fi
    if ! "${privilege[@]}" bash "$temp_file"; then
        rm -f "$temp_file"
        printf '錯誤：NodeSource 安裝腳本執行失敗。\n' >&2
        return 1
    fi
    rm -f "$temp_file"

    if ! "${privilege[@]}" "$manager" install -y nodejs; then
        printf '錯誤：Node.js 套件安裝失敗。\n' >&2
        return 1
    fi
    if ! node_version="$(node --version 2>/dev/null)"; then
        printf '錯誤：Node.js 安裝後驗證失敗。\n' >&2
        return 1
    fi
    if ! npm_version="$(npm --version 2>/dev/null)"; then
        printf '錯誤：npm 安裝後驗證失敗。\n' >&2
        return 1
    fi

    printf 'Node.js: %s\n' "$node_version"
    printf 'npm: %s\n' "$npm_version"
}

validate_python_minor() {
    case "${1:-}" in
        3.10|3.11|3.12|3.13|3.14)
            return 0
            ;;
        *)
            return 1
            ;;
    esac
}

python_runtime_root() {
    printf '%s\n' "${S12RYT_PYTHON_ROOT:-${XDG_DATA_HOME:-$HOME/.local/share}/s12ryt/python}"
}

python_uv_bin() {
    printf '%s/uv/uv\n' "$(python_runtime_root)"
}

python_version_bin_dir() {
    local effective_uid="${S12RYT_EFFECTIVE_UID:-$EUID}"

    if [[ "$effective_uid" == "0" ]]; then
        printf '/usr/local/bin\n'
    else
        printf '%s/.local/bin\n' "$HOME"
    fi
}

python_venv_path() {
    local minor="${1:-}"

    validate_python_minor "$minor" || return 1
    printf '%s/venvs/%s\n' "$(python_runtime_root)" "$minor"
}

resolve_python_command() {
    local minor="$1"
    local versioned_path

    versioned_path="$(python_version_bin_dir)/python${minor}"
    if [[ -x "$versioned_path" ]]; then
        printf '%s\n' "$versioned_path"
        return 0
    fi
    command -v "python${minor}" 2>/dev/null
}

python_reports_minor() {
    local python_command="$1"
    local minor="$2"
    local version_output

    version_output="$("$python_command" --version 2>&1)" || return 1
    [[ "$version_output" == "Python ${minor}."* ]]
}

python_has_pip() {
    "$1" -m pip --version >/dev/null 2>&1
}

fixed_python_venv_ready() {
    local minor="$1"
    local venv_python

    venv_python="$(python_venv_path "$minor")/bin/python"
    [[ -x "$venv_python" ]] || return 1
    python_reports_minor "$venv_python" "$minor" || return 1
    python_has_pip "$venv_python"
}

python_is_uv_managed() {
    local python_command="$1"
    local runtime_root resolved_path

    runtime_root="$(python_runtime_root)"
    resolved_path="$(readlink -f "$python_command" 2>/dev/null)" || return 1
    case "$resolved_path" in
        "$runtime_root/versions/"*) return 0 ;;
        *) return 1 ;;
    esac
}

require_python_privilege() {
    local effective_uid="${S12RYT_EFFECTIVE_UID:-$EUID}"

    if [[ ! "$effective_uid" =~ ^[0-9]+$ ]]; then
        printf '錯誤：無法判斷目前管理權限。\n' >&2
        return 1
    fi
    if (( effective_uid != 0 )); then
        if ! command -v sudo >/dev/null 2>&1 || ! sudo -n true >/dev/null 2>&1; then
            printf '錯誤：Python 安裝需要 root 權限或可用的 sudo。\n' >&2
            return 1
        fi
    fi
}

ensure_private_uv() {
    local runtime_root uv_bin uv_dir temp_file uv_version
    local installer_url='https://releases.astral.sh/github/uv/releases/download/0.12.1/uv-installer.sh'

    runtime_root="$(python_runtime_root)"
    uv_bin="$(python_uv_bin)"
    uv_dir="${runtime_root}/uv"
    if [[ -x "$uv_bin" ]]; then
        uv_version="$("$uv_bin" --version 2>/dev/null || true)"
        if [[ "$uv_version" == "uv 0.12.1" ]]; then
            return 0
        fi
    fi

    if ! command -v curl >/dev/null 2>&1; then
        printf '錯誤：下載 uv 安裝腳本需要 curl。\n' >&2
        return 1
    fi
    mkdir -p "$runtime_root" || {
        printf '錯誤：無法建立 Python 執行環境目錄。\n' >&2
        return 1
    }
    temp_file="$(mktemp "${TMPDIR:-/tmp}/s12ryt-uv.XXXXXX")" || {
        printf '錯誤：無法建立 uv 暫存檔。\n' >&2
        return 1
    }
    if ! curl -fsSL --connect-timeout 5 --max-time 30 "$installer_url" -o "$temp_file"; then
        rm -f "$temp_file"
        printf '錯誤：無法下載 uv 安裝腳本。\n' >&2
        return 1
    fi
    if ! bash -n "$temp_file"; then
        rm -f "$temp_file"
        printf '錯誤：uv 安裝腳本語法驗證失敗。\n' >&2
        return 1
    fi
    if ! UV_INSTALL_DIR="$uv_dir" UV_NO_MODIFY_PATH=1 bash "$temp_file"; then
        rm -f "$temp_file"
        printf '錯誤：uv 0.12.1 安裝失敗。\n' >&2
        return 1
    fi
    rm -f "$temp_file"
    uv_version="$("$uv_bin" --version 2>/dev/null || true)"
    if [[ "$uv_version" != "uv 0.12.1" ]]; then
        printf '錯誤：uv 0.12.1 安裝後驗證失敗。\n' >&2
        return 1
    fi
}

create_fixed_python_venv() {
    local minor="$1"
    local python_selector="$2"
    local runtime_root uv_bin venv_path venv_python

    runtime_root="$(python_runtime_root)"
    uv_bin="$(python_uv_bin)"
    venv_path="$(python_venv_path "$minor")"
    venv_python="${venv_path}/bin/python"
    if ! UV_PYTHON_INSTALL_DIR="${runtime_root}/versions" \
        UV_PYTHON_BIN_DIR="$(python_version_bin_dir)" \
        "$uv_bin" venv --python "$python_selector" --seed "$venv_path"; then
        printf '錯誤：Python %s 固定 venv 建立失敗。\n' "$minor" >&2
        return 1
    fi
    if ! python_reports_minor "$venv_python" "$minor" || ! python_has_pip "$venv_python"; then
        printf '錯誤：Python %s 固定 venv 驗證失敗。\n' "$minor" >&2
        return 1
    fi
}

install_python() {
    local minor confirmation manager runtime_root uv_bin bin_dir venv_path
    local python_command="" python_version="" pip_version="" python_selector=""
    local existing_python=false direct_pip_ready=false venv_ready=false managed_python=false

    printf '可選版本：3.10、3.11、3.12、3.13、3.14\n'
    printf '請輸入 Python minor: '
    minor=""
    IFS= read -r minor || true
    if ! validate_python_minor "$minor"; then
        printf '錯誤：只支援 Python 3.10、3.11、3.12、3.13 或 3.14。\n' >&2
        return 1
    fi

    runtime_root="$(python_runtime_root)"
    uv_bin="$(python_uv_bin)"
    bin_dir="$(python_version_bin_dir)"
    venv_path="$(python_venv_path "$minor")"
    if python_command="$(resolve_python_command "$minor")"; then
        existing_python=true
        if ! python_reports_minor "$python_command" "$minor"; then
            printf '錯誤：既有 python%s 版本驗證失敗。\n' "$minor" >&2
            return 1
        fi
        python_version="$("$python_command" --version 2>&1)"
        python_has_pip "$python_command" && direct_pip_ready=true
        fixed_python_venv_ready "$minor" && venv_ready=true
        python_is_uv_managed "$python_command" && managed_python=true
        if [[ "$direct_pip_ready" == true && "$venv_ready" == true ]]; then
            printf '%s 已完整安裝，略過所有變更。\n' "$python_version"
            return 0
        fi

        printf '偵測到 %s，但缺少：' "$python_version"
        [[ "$direct_pip_ready" == true ]] || printf ' direct pip'
        [[ "$venv_ready" == true ]] || printf ' 固定 venv'
        printf '。\n'
        printf '是否補齊缺少項目？ [y/N]: '
    else
        if ! manager="$(detect_package_manager)"; then
            printf '錯誤：找不到支援的套件管理器。\n' >&2
            return 1
        fi
        printf '套件管理器: %s\n' "$manager"
        printf 'Python 將由專案私有 uv 0.12.1 管理。\n'
        printf '是否安裝 Python %s？ [y/N]: ' "$minor"
    fi

    confirmation=""
    IFS= read -r confirmation || true
    case "$confirmation" in
        y|Y|yes|YES)
            ;;
        *)
            if [[ "$existing_python" == true ]]; then
                printf '已取消補齊 Python %s。\n' "$minor"
            else
                printf '已取消 Python 安裝。\n'
            fi
            return 0
            ;;
    esac

    require_python_privilege || return 1
    ensure_private_uv || return 1
    mkdir -p "$bin_dir" "${runtime_root}/versions" "${runtime_root}/venvs" || {
        printf '錯誤：無法建立 Python 安裝目錄。\n' >&2
        return 1
    }

    if [[ "$existing_python" != true ]]; then
        if ! UV_PYTHON_INSTALL_DIR="${runtime_root}/versions" \
            UV_PYTHON_BIN_DIR="$bin_dir" \
            "$uv_bin" python install "$minor"; then
            printf '錯誤：Python %s 安裝失敗。\n' "$minor" >&2
            return 1
        fi
        python_command="${bin_dir}/python${minor}"
        if [[ ! -x "$python_command" ]] || ! python_reports_minor "$python_command" "$minor"; then
            printf '錯誤：Python %s 安裝後驗證失敗。\n' "$minor" >&2
            return 1
        fi
        managed_python=true
        direct_pip_ready=false
    fi

    if [[ "$managed_python" == true ]]; then
        if ! python_has_pip "$python_command"; then
            if ! "$python_command" -m ensurepip; then
                printf '錯誤：Python %s pip 補齊失敗。\n' "$minor" >&2
                return 1
            fi
        fi
        if ! pip_version="$("$python_command" -m pip --version 2>/dev/null)"; then
            printf '錯誤：Python %s pip 驗證失敗。\n' "$minor" >&2
            return 1
        fi
        python_selector="$minor"
    else
        python_selector="$python_command"
        if [[ "$direct_pip_ready" != true ]]; then
            printf '提示：既有 Python 非 uv 管理，直接 pip 仍由系統安裝狀態決定。\n'
        else
            pip_version="$("$python_command" -m pip --version 2>/dev/null || true)"
        fi
    fi

    if [[ "$venv_ready" != true ]]; then
        create_fixed_python_venv "$minor" "$python_selector" || return 1
    fi
    if ! python_version="$("$python_command" --version 2>&1)"; then
        printf '錯誤：Python %s 最終版本驗證失敗。\n' "$minor" >&2
        return 1
    fi
    if [[ -z "$pip_version" && "$managed_python" == true ]]; then
        printf '錯誤：Python %s pip 驗證結果為空。\n' "$minor" >&2
        return 1
    fi

    printf '%s\n' "$python_version"
    [[ -z "$pip_version" ]] || printf '%s\n' "$pip_version"
    printf '固定 venv: %s\n' "$venv_path"
    printf 'Python %s 安裝完成。\n' "$minor"
}

json_get() {
    local json_file="$1"
    local json_path="$2"
    local value

    if command -v jq >/dev/null 2>&1; then
        value="$(jq -r "$json_path" "$json_file" 2>/dev/null)" || return 1
        [[ "$value" != "null" ]] || return 1
        printf '%s\n' "$value"
        return
    fi
    if command -v python3 >/dev/null 2>&1; then
        python3 - "$json_file" "$json_path" <<'PY'
import json
import sys

try:
    with open(sys.argv[1], encoding="utf-8") as source:
        value = json.load(source)
except (OSError, ValueError):
    raise SystemExit(1)
for key in sys.argv[2].lstrip(".").split("."):
    if not isinstance(value, dict) or key not in value:
        raise SystemExit(1)
    value = value[key]
if value is None:
    raise SystemExit(1)
if isinstance(value, bool):
    print(str(value).lower())
elif isinstance(value, (dict, list)):
    print(json.dumps(value, ensure_ascii=False))
else:
    print(value)
PY
        return
    fi

    printf '錯誤：IP 資訊需要 jq 或 python3 才能解析 JSON。\n' >&2
    return 1
}

boolean_label() {
    case "$1" in
        true) printf '是\n' ;;
        false) printf '否\n' ;;
        *) printf '未知\n' ;;
    esac
}

print_ip_record() {
    local family="$1"
    local json_file="$2"
    local ip error asn isp country region datacenter mobile proxy vpn

    error="$(json_get "$json_file" .error 2>/dev/null || true)"
    if [[ -n "$error" ]]; then
        printf '%s: API 錯誤：%s\n' "$family" "$error" >&2
        return 1
    fi
    if ! ip="$(json_get "$json_file" .ip)"; then
        printf '%s: 無法解析 IP 資訊。\n' "$family" >&2
        return 1
    fi

    asn="$(json_get "$json_file" .asn.asn 2>/dev/null || true)"
    isp="$(json_get "$json_file" .asn.org 2>/dev/null || true)"
    [[ -n "$isp" ]] || isp="$(json_get "$json_file" .company.name 2>/dev/null || true)"
    country="$(json_get "$json_file" .location.country 2>/dev/null || true)"
    region="$(json_get "$json_file" .location.state 2>/dev/null || true)"
    datacenter="$(json_get "$json_file" .is_datacenter 2>/dev/null || true)"
    mobile="$(json_get "$json_file" .is_mobile 2>/dev/null || true)"
    proxy="$(json_get "$json_file" .is_proxy 2>/dev/null || true)"
    vpn="$(json_get "$json_file" .is_vpn 2>/dev/null || true)"

    [[ "$asn" == AS* || -z "$asn" ]] || asn="AS${asn}"
    printf '%s: %s\n' "$family" "$ip"
    printf '  ASN: %s\n' "${asn:-未知}"
    printf '  ISP: %s\n' "${isp:-未知}"
    if [[ -n "$country" && -n "$region" ]]; then
        printf '  國家/地區: %s / %s\n' "$country" "$region"
    else
        printf '  國家/地區: %s\n' "${country:-未知}"
    fi
    printf '  資料中心: %s\n' "$(boolean_label "$datacenter")"
    printf '  行動網路: %s\n' "$(boolean_label "$mobile")"
    printf '  Proxy: %s\n' "$(boolean_label "$proxy")"
    printf '  VPN: %s\n' "$(boolean_label "$vpn")"
    if [[ "$datacenter" == "false" && "$mobile" == "false" && \
        "$proxy" == "false" && "$vpn" == "false" ]]; then
        printf '  判定: 可能家寬（僅為推測）\n'
    fi
}

classify_http_result() {
    local curl_status="$1"
    local http_code="$2"

    if (( curl_status != 0 )); then
        printf '逾時/失敗\n'
        return
    fi
    case "$http_code" in
        2??|3??) printf '可達\n' ;;
        *) printf '受限\n' ;;
    esac
}

check_connectivity() {
    local index code curl_status
    local -a names=(GitHub Google Cloudflare YouTube Netflix Disney+ Spotify TikTok ChatGPT Gemini Telegram)
    local -a urls=(
        'https://github.com/'
        'https://www.google.com/'
        'https://www.cloudflare.com/'
        'https://www.youtube.com/'
        'https://www.netflix.com/'
        'https://www.disneyplus.com/'
        'https://www.spotify.com/'
        'https://www.tiktok.com/'
        'https://chatgpt.com/'
        'https://gemini.google.com/'
        'https://telegram.org/'
    )

    printf '\n站點連通性：\n'
    for index in "${!names[@]}"; do
        code="$(curl -sS -L -o /dev/null --connect-timeout 5 --max-time 10 \
            -w '%{http_code}' "${urls[$index]}" 2>/dev/null)"
        curl_status=$?
        printf '  %s: %s\n' "${names[$index]}" \
            "$(classify_http_result "$curl_status" "${code:-000}")"
    done
}

extract_jsonish_region() {
    local key="$1"
    local response_file="$2"

    sed -nE 's/.*"'"$key"'"[[:space:]]*:[[:space:]]*"([A-Za-z]{2})".*/\1/p' \
        "$response_file" | head -n 1 | tr '[:lower:]' '[:upper:]'
}

parse_stream_region() {
    local service="$1"
    local response_file="$2"
    local region=""

    case "$service" in
        Netflix|Spotify)
            region="$(extract_jsonish_region country "$response_file")"
            ;;
        Disney+|'YouTube Premium'|Gemini)
            region="$(extract_jsonish_region countryCode "$response_file")"
            ;;
        TikTok)
            region="$(extract_jsonish_region region "$response_file")"
            ;;
        ChatGPT)
            region="$(awk -F= '$1 == "loc" && $2 ~ /^[A-Za-z][A-Za-z]$/ {
                print toupper($2); exit
            }' "$response_file")"
            ;;
    esac

    [[ -n "$region" ]] || return 1
    printf '%s\n' "$region"
}

render_stream_result() {
    local service="$1"
    local curl_status="$2"
    local http_code="$3"
    local region="$4"
    local availability

    availability="$(classify_http_result "$curl_status" "$http_code")"
    if [[ "$availability" == "可達" && -n "$region" ]]; then
        printf '  %s: 可達（推測地區: %s）\n' "$service" "$region"
    elif [[ "$availability" == "可達" ]]; then
        printf '  %s: 可達（地區未知）\n' "$service"
    else
        printf '  %s: %s\n' "$service" "$availability"
    fi
}

check_streaming_services() {
    local fallback_region="${1:-}"
    local temp_dir index body code curl_status region
    local -a names=(Netflix Disney+ 'YouTube Premium' Spotify TikTok ChatGPT Gemini)
    local -a urls=(
        'https://www.netflix.com/title/80018499'
        'https://www.disneyplus.com/'
        'https://www.youtube.com/premium'
        'https://www.spotify.com/api/growth-targeting/v1/sdk/non-authenticated-user'
        'https://www.tiktok.com/'
        'https://chatgpt.com/cdn-cgi/trace'
        'https://gemini.google.com/'
    )

    temp_dir="$(mktemp -d)" || return 1
    printf '\n有限服務可用性與地區檢測：\n'
    for index in "${!names[@]}"; do
        body="${temp_dir}/response-${index}"
        code="$(curl -sS -L -A 'Mozilla/5.0 (X11; Linux x86_64) s12ryt/1.0' \
            --connect-timeout 5 --max-time 12 -o "$body" -w '%{http_code}' \
            "${urls[$index]}" 2>/dev/null)"
        curl_status=$?
        region=""
        if (( curl_status == 0 )) && [[ "$code" == 2?? || "$code" == 3?? ]]; then
            region="$(parse_stream_region "${names[$index]}" "$body" 2>/dev/null || true)"
            region="${region:-$fallback_region}"
        fi
        render_stream_result "${names[$index]}" "$curl_status" "${code:-000}" "$region"
    done
    rm -rf "$temp_dir"
}

show_network_information() {
    local temp_dir family family_name response_file country_code=""

    if ! command -v curl >/dev/null 2>&1; then
        printf '錯誤：IP 與連通性檢測需要 curl。\n' >&2
        return 1
    fi
    if ! command -v jq >/dev/null 2>&1 && ! command -v python3 >/dev/null 2>&1; then
        printf '錯誤：IP 資訊需要 jq 或 python3 才能解析 JSON。\n' >&2
        return 1
    fi

    temp_dir="$(mktemp -d)" || return 1
    printf 'IP 資訊（來源：ipapi.is）：\n'
    for family in 4 6; do
        family_name="IPv${family}"
        response_file="${temp_dir}/ipapi-${family}.json"
        if curl "-${family}" -fsS --connect-timeout 5 --max-time 12 \
            "${S12RYT_IPAPI_URL:-https://api.ipapi.is/}" -o "$response_file"; then
            print_ip_record "$family_name" "$response_file" || true
            if [[ -z "$country_code" ]]; then
                country_code="$(json_get "$response_file" .location.country_code 2>/dev/null || true)"
            fi
        else
            printf '%s: 無法取得。\n' "$family_name"
        fi
    done
    rm -rf "$temp_dir"

    check_connectivity
    check_streaming_services "$country_code"
    printf '\n提醒：家寬與地區均為推測；網站及非公開端點可能隨時變更。\n'
    printf '結果只代表檢測當下，不保證登入後可播放。\n'
}

script_path() {
    local source_path="${BASH_SOURCE[0]}"

    if command -v readlink >/dev/null 2>&1; then
        readlink -f "$source_path" 2>/dev/null && return 0
    fi

    printf '%s/%s\n' "$(cd "$(dirname "$source_path")" && pwd)" "$(basename "$source_path")"
}

script_source_is_temporary() {
    local source_path="${BASH_SOURCE[0]:-}"

    case "$source_path" in
        /dev/fd/*|/proc/*/fd/*|/dev/stdin)
            return 0
            ;;
    esac
    [[ -z "$source_path" || ! -f "$source_path" ]]
}

proot_helper_path() {
    if [[ -n "${S12RYT_PROOT_HELPER_PATH:-}" ]]; then
        printf '%s\n' "$S12RYT_PROOT_HELPER_PATH"
    elif (( EUID == 0 )); then
        printf '/usr/local/lib/s12ryt/install-proot.sh\n'
    else
        printf '%s/.local/share/s12ryt/install-proot.sh\n' "$HOME"
    fi
}

ensure_proot_helper() {
    local source_path target_path target_dir temp_file helper_url

    target_path="$(proot_helper_path)"
    target_dir="$(dirname "$target_path")"
    source_path="${S12RYT_PROOT_HELPER_SOURCE:-$(dirname "$(script_path)")/install-proot.sh}"
    mkdir -p "$target_dir" || {
        printf '錯誤：無法建立 PRoot 腳本目錄。\n' >&2
        return 1
    }

    if [[ "$source_path" == "$target_path" && -f "$target_path" ]]; then
        bash -n "$target_path" || {
            printf '錯誤：既有 PRoot 腳本語法無效。\n' >&2
            return 1
        }
        chmod 0755 "$target_path"
        printf '%s\n' "$target_path"
        return 0
    fi

    temp_file="$(mktemp "${target_dir}/.install-proot.XXXXXX")" || return 1
    if [[ -f "$source_path" ]]; then
        if ! cp "$source_path" "$temp_file"; then
            rm -f "$temp_file"
            printf '錯誤：無法複製 PRoot 腳本。\n' >&2
            return 1
        fi
    else
        if ! command -v curl >/dev/null 2>&1; then
            rm -f "$temp_file"
            printf '錯誤：下載 PRoot 腳本需要 curl。\n' >&2
            return 1
        fi
        helper_url="${S12RYT_PROOT_HELPER_URL:-https://raw.githubusercontent.com/s12ryt/s12ryt-vps-sh/v${VERSION}/install-proot.sh}"
        if ! curl -fsSL --connect-timeout 5 --max-time 30 "$helper_url" -o "$temp_file"; then
            rm -f "$temp_file"
            printf '錯誤：無法下載 PRoot 腳本。\n' >&2
            return 1
        fi
    fi

    if ! bash -n "$temp_file"; then
        rm -f "$temp_file"
        printf '錯誤：PRoot 腳本語法驗證失敗，保留既有版本。\n' >&2
        return 1
    fi
    chmod 0755 "$temp_file"
    if ! mv -f "$temp_file" "$target_path"; then
        rm -f "$temp_file"
        printf '錯誤：無法原子安裝 PRoot 腳本。\n' >&2
        return 1
    fi
    printf '%s\n' "$target_path"
}

prepare_proot_script() {
    local helper

    helper="$(ensure_proot_helper)" || return 1
    bash "$helper" setup
}

run_proot_manager() {
    local helper

    helper="$(ensure_proot_helper)" || return 1
    bash "$helper" manage
}

run_supervisor_manager() {
    local helper

    helper="$(ensure_proot_helper)" || return 1
    bash "$helper" service
}

ipv6_helper_path() {
    if [[ -n "${S12RYT_IPV6_HELPER_PATH:-}" ]]; then
        printf '%s\n' "$S12RYT_IPV6_HELPER_PATH"
    elif (( EUID == 0 )); then
        printf '/usr/local/lib/s12ryt/install-ipv6.sh\n'
    else
        printf '%s/.local/share/s12ryt/install-ipv6.sh\n' "$HOME"
    fi
}

ensure_ipv6_helper() {
    local source_path target_path target_dir temp_file helper_url

    target_path="$(ipv6_helper_path)"
    target_dir="$(dirname "$target_path")"
    source_path="${S12RYT_IPV6_HELPER_SOURCE:-$(dirname "$(script_path)")/install-ipv6.sh}"
    mkdir -p -- "$target_dir" || {
        printf '錯誤：無法建立 IPv6 專案腳本目錄。\n' >&2
        return 1
    }

    if [[ "$source_path" == "$target_path" && -f "$target_path" ]]; then
        if ! bash -n "$target_path"; then
            printf '錯誤：既有 IPv6 專案腳本語法無效。\n' >&2
            return 1
        fi
        chmod 0755 "$target_path" || return 1
        printf '%s\n' "$target_path"
        return 0
    fi

    temp_file="$(mktemp "${target_dir}/.install-ipv6.XXXXXX")" || {
        printf '錯誤：無法建立 IPv6 專案腳本暫存檔。\n' >&2
        return 1
    }
    if [[ -f "$source_path" ]]; then
        if ! cp -- "$source_path" "$temp_file"; then
            rm -f -- "$temp_file"
            printf '錯誤：無法複製 IPv6 專案腳本。\n' >&2
            return 1
        fi
    else
        if ! command -v curl >/dev/null 2>&1; then
            rm -f -- "$temp_file"
            printf '錯誤：下載 IPv6 專案腳本需要 curl。\n' >&2
            return 1
        fi
        helper_url="${S12RYT_IPV6_HELPER_URL:-https://raw.githubusercontent.com/s12ryt/s12ryt-vps-sh/v${VERSION}/install-ipv6.sh}"
        if ! curl -fsSL --connect-timeout 5 --max-time 30 "$helper_url" -o "$temp_file"; then
            rm -f -- "$temp_file"
            printf '錯誤：無法下載 IPv6 專案腳本。\n' >&2
            return 1
        fi
    fi

    if ! bash -n "$temp_file"; then
        rm -f -- "$temp_file"
        printf '錯誤：IPv6 專案腳本語法驗證失敗，保留既有版本。\n' >&2
        return 1
    fi
    if ! chmod 0755 "$temp_file" || ! mv -f -- "$temp_file" "$target_path"; then
        rm -f -- "$temp_file"
        printf '錯誤：無法原子安裝 IPv6 專案腳本。\n' >&2
        return 1
    fi
    printf '%s\n' "$target_path"
}

detect_fanout_init() {
    if [[ -n "${S12RYT_INIT_SYSTEM+x}" ]]; then
        case "$S12RYT_INIT_SYSTEM" in
            systemd|openrc)
                printf '%s\n' "$S12RYT_INIT_SYSTEM"
                return 0
                ;;
            *)
                return 1
                ;;
        esac
    fi

    if [[ -d /run/systemd/system ]] && command -v systemctl >/dev/null 2>&1; then
        printf 'systemd\n'
        return 0
    fi
    if command -v rc-service >/dev/null 2>&1; then
        printf 'openrc\n'
        return 0
    fi
    return 1
}

check_fanout_prerequisites() {
    local kernel_name="${S12RYT_KERNEL_NAME:-$(uname -s)}"
    local effective_uid="${S12RYT_EFFECTIVE_UID:-$EUID}"
    local tun_path="${S12RYT_TUN_PATH:-/dev/net/tun}"

    if [[ "$kernel_name" != "Linux" ]]; then
        printf '錯誤：Fanout 只支援 Linux。\n' >&2
        return 1
    fi
    if [[ ! "$effective_uid" =~ ^[0-9]+$ || "$effective_uid" != "0" ]]; then
        printf '錯誤：Fanout 安裝需要 root 權限。\n' >&2
        return 1
    fi
    if [[ ! -e "$tun_path" ]]; then
        printf '錯誤：Fanout 需要可用的 /dev/net/tun。\n' >&2
        return 1
    fi
    if ! command -v unshare >/dev/null 2>&1 || ! unshare -n true >/dev/null 2>&1; then
        printf '錯誤：Fanout 需要可用的 network namespace。\n' >&2
        return 1
    fi
    if ! detect_fanout_init >/dev/null; then
        printf '錯誤：Fanout 需要 systemd 或 OpenRC。\n' >&2
        return 1
    fi
}

install_fanout() {
    local installer_url temp_file

    check_fanout_prerequisites || return 1
    if ! command -v curl >/dev/null 2>&1; then
        printf '錯誤：下載 Fanout 安裝腳本需要 curl。\n' >&2
        return 1
    fi
    temp_file="$(mktemp "${TMPDIR:-/tmp}/s12ryt-fanout.XXXXXX")" || {
        printf '錯誤：無法建立 Fanout 暫存檔。\n' >&2
        return 1
    }
    installer_url="${S12RYT_FANOUT_URL:-https://raw.githubusercontent.com/byJoey/fanout/main/install.sh}"
    if ! curl -fsSL --connect-timeout 5 --max-time 30 "$installer_url" -o "$temp_file"; then
        rm -f "$temp_file"
        printf '錯誤：無法下載 Fanout 安裝腳本。\n' >&2
        return 1
    fi
    if ! bash -n "$temp_file"; then
        rm -f "$temp_file"
        printf '錯誤：Fanout 安裝腳本語法驗證失敗。\n' >&2
        return 1
    fi
    if ! bash "$temp_file"; then
        rm -f "$temp_file"
        printf '錯誤：Fanout 上游安裝腳本執行失敗。\n' >&2
        return 1
    fi
    rm -f "$temp_file"
    printf 'Fanout 上游安裝腳本執行完成。\n'
}

validate_version() {
    [[ "$1" =~ ^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$ ]]
}

version_is_newer() {
    local current="$1"
    local candidate="$2"
    local index
    local -a current_parts candidate_parts

    validate_version "$current" || return 2
    validate_version "$candidate" || return 2
    IFS=. read -r -a current_parts <<< "$current"
    IFS=. read -r -a candidate_parts <<< "$candidate"
    for index in 0 1 2; do
        if (( ${#candidate_parts[index]} > ${#current_parts[index]} )); then
            return 0
        fi
        if (( ${#candidate_parts[index]} < ${#current_parts[index]} )); then
            return 1
        fi
        if [[ "${candidate_parts[index]}" > "${current_parts[index]}" ]]; then
            return 0
        fi
        if [[ "${candidate_parts[index]}" < "${current_parts[index]}" ]]; then
            return 1
        fi
    done
    return 1
}

update_target_path() {
    if [[ -n "${S12RYT_UPDATE_TARGET:-}" ]]; then
        printf '%s\n' "$S12RYT_UPDATE_TARGET"
    elif (( EUID == 0 )); then
        printf '/usr/local/lib/s12ryt/s12ryt.sh\n'
    else
        printf '%s/.local/share/s12ryt/s12ryt.sh\n' "$HOME"
    fi
}

extract_script_version() {
    sed -nE 's/^[[:space:]]*(readonly[[:space:]]+)?VERSION="([0-9]+\.[0-9]+\.[0-9]+)"[[:space:]]*$/\2/p' \
        "$1" | head -n 1
}

check_for_updates() {
    local target target_dir temp_dir api_file downloaded release_tag release_version downloaded_version
    local api_url download_url

    if ! command -v curl >/dev/null 2>&1; then
        printf '錯誤：檢查更新需要 curl。\n' >&2
        return 1
    fi
    if ! command -v jq >/dev/null 2>&1 && ! command -v python3 >/dev/null 2>&1; then
        printf '錯誤：檢查更新需要 jq 或 python3。\n' >&2
        return 1
    fi

    target="$(update_target_path)"
    target_dir="$(dirname "$target")"
    if [[ ! -d "$target_dir" || ! -w "$target_dir" ]]; then
        printf '錯誤：穩定副本目錄不存在或不可寫入。\n' >&2
        return 1
    fi
    temp_dir="$(mktemp -d "${target_dir}/.s12ryt-update.XXXXXX")" || {
        printf '錯誤：無法建立更新暫存檔。\n' >&2
        return 1
    }
    api_file="${temp_dir}/release.json"
    downloaded="${temp_dir}/s12ryt.sh"
    api_url="${S12RYT_RELEASE_API_URL:-https://api.github.com/repos/s12ryt/s12ryt-vps-sh/releases/latest}"
    if ! curl -fsSL --connect-timeout 5 --max-time 15 "$api_url" -o "$api_file"; then
        rm -rf "$temp_dir"
        printf '錯誤：無法查詢最新 Release。\n' >&2
        return 1
    fi
    release_tag="$(json_get "$api_file" .tag_name 2>/dev/null || true)"
    if [[ "$release_tag" != v* ]]; then
        rm -rf "$temp_dir"
        printf '錯誤：Release tag 格式無效。\n' >&2
        return 1
    fi
    release_version="${release_tag#v}"
    if ! validate_version "$release_version"; then
        rm -rf "$temp_dir"
        printf '錯誤：Release tag 格式無效。\n' >&2
        return 1
    fi
    if ! version_is_newer "$VERSION" "$release_version"; then
        rm -rf "$temp_dir"
        printf '目前已是最新版 v%s。\n' "$VERSION"
        return 0
    fi

    download_url="${S12RYT_UPDATE_BASE_URL:-https://raw.githubusercontent.com/s12ryt/s12ryt-vps-sh}/${release_tag}/s12ryt.sh"
    if ! curl -fsSL --connect-timeout 5 --max-time 30 "$download_url" -o "$downloaded"; then
        rm -rf "$temp_dir"
        printf '錯誤：無法下載新版腳本。\n' >&2
        return 1
    fi
    if ! bash -n "$downloaded"; then
        rm -rf "$temp_dir"
        printf '錯誤：新版腳本語法驗證失敗。\n' >&2
        return 1
    fi
    downloaded_version="$(extract_script_version "$downloaded")"
    if [[ "$downloaded_version" != "$release_version" ]]; then
        rm -rf "$temp_dir"
        printf '錯誤：下載版本與 Release tag 不一致。\n' >&2
        return 1
    fi
    chmod 0755 "$downloaded" || {
        rm -rf "$temp_dir"
        printf '錯誤：無法設定新版腳本權限。\n' >&2
        return 1
    }
    if ! mv -f "$downloaded" "$target"; then
        rm -rf "$temp_dir"
        printf '錯誤：無法原子替換穩定副本。\n' >&2
        return 1
    fi
    rm -rf "$temp_dir"
    printf '已更新至 v%s。重新執行 s 即可使用新版。\n' "$release_version"
}

show_projects() {
    local choice

    while true; do
        cat <<'EOF'
-----
s12ryt 項目列表
-----
1. s12ryt-多ipv6出站
0. 返回
-----
EOF
        printf '輸入選項: '
        if ! IFS= read -r choice; then
            printf '\n'
            return 0
        fi
        case "$choice" in
            0)
                return 0
                ;;
            1)
                run_ipv6_project_menu || true
                ;;
            *)
                printf '無效選項，請重新輸入。\n' >&2
                ;;
        esac
    done
}

install_ipv6_project() {
    local helper

    helper="$(ensure_ipv6_helper)" || return 1
    bash "$helper" install
}

update_ipv6_project() {
    local helper

    helper="$(ensure_ipv6_helper)" || return 1
    bash "$helper" update
}

uninstall_ipv6_project() {
    local helper

    helper="$(ensure_ipv6_helper)" || return 1
    bash "$helper" uninstall
}

run_ipv6_project_menu() {
    local choice

    while true; do
        cat <<'EOF'
-----
        s12ryt-ipv6（多 IPv6 出站）
-----
        1. 安裝
        2. 更新
        3. 卸載
0. 退出
-----
EOF
        printf '輸入選項: '
        if ! IFS= read -r choice; then
            printf '\n'
            return 0
        fi
        case "$choice" in
            0)
                return 0
                ;;
            1)
                install_ipv6_project || true
                ;;
            2)
                update_ipv6_project || true
                ;;
            3)
                uninstall_ipv6_project || true
                ;;
            *)
                printf '無效選項，請重新輸入。\n' >&2
                ;;
        esac
    done
}

install_downloaded_bootstrap() {
    local stable_path="$1"
    local stable_dir temp_file bootstrap_url downloaded_version

    stable_dir="$(dirname "$stable_path")"
    if ! command -v curl >/dev/null 2>&1; then
        printf '錯誤：重新下載 s12ryt 完整腳本需要 curl。\n' >&2
        return 1
    fi
    temp_file="$(mktemp "${stable_dir}/.s12ryt-bootstrap.XXXXXX")" || {
        printf '錯誤：無法建立 s12ryt 重新下載暫存檔。\n' >&2
        return 1
    }
    bootstrap_url="${S12RYT_BOOTSTRAP_URL:-https://raw.githubusercontent.com/s12ryt/s12ryt-vps-sh/main/s12ryt.sh}"
    if ! curl -fsSL --connect-timeout 5 --max-time 30 "$bootstrap_url" -o "$temp_file"; then
        rm -f "$temp_file"
        printf '錯誤：無法重新下載 s12ryt 完整腳本。\n' >&2
        return 1
    fi
    if ! bash -n "$temp_file"; then
        rm -f "$temp_file"
        printf '錯誤：重新下載的 s12ryt 腳本語法驗證失敗。\n' >&2
        return 1
    fi
    downloaded_version="$(extract_script_version "$temp_file")"
    if [[ "$downloaded_version" != "$VERSION" ]]; then
        rm -f "$temp_file"
        printf '錯誤：重新下載的 s12ryt 腳本版本與目前執行版本不一致。\n' >&2
        return 1
    fi
    if ! chmod 0755 "$temp_file" || ! mv -f "$temp_file" "$stable_path"; then
        rm -f "$temp_file"
        printf '錯誤：無法原子安裝重新下載的 s12ryt 腳本。\n' >&2
        return 1
    fi
}

install_launcher() {
    local source_path stable_path launcher_path launcher_dir stable_dir temp_launcher
    local source_helper stable_helper helper_temp
    local temporary_source=0

    if script_source_is_temporary; then
        temporary_source=1
        source_path=""
    else
        source_path="$(script_path)"
    fi
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

    if (( temporary_source == 1 )); then
        if ! install_downloaded_bootstrap "$stable_path"; then
            return 2
        fi
        source_path="$stable_path"
    elif [[ "$source_path" != "$stable_path" ]]; then
        if ! cp "$source_path" "$stable_path" || ! chmod 0755 "$stable_path"; then
            printf '錯誤：無法建立 s12ryt 穩定副本。\n' >&2
            return 1
        fi
    fi

    source_helper="$(dirname "$source_path")/install-proot.sh"
    stable_helper="${stable_dir}/install-proot.sh"
    if [[ -f "$source_helper" && "$source_helper" != "$stable_helper" ]]; then
        helper_temp="$(mktemp "${stable_dir}/.install-proot.XXXXXX")" || return 1
        if ! cp "$source_helper" "$helper_temp" || ! bash -n "$helper_temp"; then
            rm -f "$helper_temp"
            printf '錯誤：無法建立 PRoot 腳本穩定副本。\n' >&2
            return 1
        fi
        chmod 0755 "$helper_temp"
        mv -f "$helper_temp" "$stable_helper"
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

terminal_ui_enabled() {
    case "${S12RYT_FORCE_INTERACTIVE:-}" in
        1)
            return 0
            ;;
        0)
            return 1
            ;;
    esac

    [[ -t 0 && -t 1 && "${TERM:-dumb}" != "dumb" ]]
}

clear_terminal_history() {
    terminal_ui_enabled || return 0
    printf '\033[2J\033[3J\033[H'
}

wait_for_return_key() {
    local key=""

    terminal_ui_enabled || return 0
    printf '按隨意鍵以返回腳本'
    IFS= read -r -s -n 1 key || true
    : "$key"
    printf '\n'
}

run_menu_action() {
    local action_status=0

    clear_terminal_history
    "$@" || action_status=$?
    wait_for_return_key
    clear_terminal_history
    return "$action_status"
}

print_menu() {
    cat <<EOF
-----
s12ryt 的 VPS 腳本
版本: v${VERSION}
Copyright (C) 2026 s12ryt
授權: GPL-3.0-only；本程式不提供任何擔保，詳見 LICENSE。
-----
1. 系統資訊
2. 更新系統
3. IP 資訊
4. 自動 PRoot（腳本）
5. 自動 PRoot（安裝虛擬機）
6. 自動偽造 systemd
7. 自動安裝 Joey 的 fanout
8. s12ryt 項目列表
9. 安裝 Python
10. 安裝 Node.js
-----
11. 檢查更新
-----
0. 退出
-----
EOF
}

not_implemented() {
    printf '此功能將在後續版本完成。\n'
}

main() {
    local choice install_status

    if install_launcher; then
        :
    else
        install_status=$?
        if (( install_status == 2 )); then
            printf '警告：僅臨時執行；s 可能不存在或仍是舊版。\n' >&2
        else
            return 1
        fi
    fi
    clear_terminal_history
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
                run_menu_action show_system_info || true
                ;;
            2)
                run_menu_action update_system || true
                ;;
            3)
                run_menu_action show_network_information || true
                ;;
            4)
                run_menu_action prepare_proot_script || true
                ;;
            5)
                run_menu_action run_proot_manager || true
                ;;
            6)
                run_menu_action run_supervisor_manager || true
                ;;
            7)
                run_menu_action install_fanout || true
                ;;
            8)
                run_menu_action show_projects || true
                ;;
            9)
                run_menu_action install_python || true
                ;;
            10)
                run_menu_action install_nodejs || true
                ;;
            11)
                run_menu_action check_for_updates || true
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
