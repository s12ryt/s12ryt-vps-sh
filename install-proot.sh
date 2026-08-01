#!/usr/bin/env bash

set -u

readonly PROOT_DISTRO_VERSION="5.5.0"

configure_proot_environment() {
    local root

    if [[ -n "${S12RYT_PROOT_ROOT:-}" ]]; then
        root="$S12RYT_PROOT_ROOT"
    else
        root="${XDG_DATA_HOME:-$HOME/.local/share}/s12ryt/proot"
        export S12RYT_PROOT_ROOT="$root"
    fi
    export XDG_DATA_HOME="${root}/data"
    export XDG_CACHE_HOME="${root}/cache"
    mkdir -p "$XDG_DATA_HOME" "$XDG_CACHE_HOME"
    printf 'PRoot 根目錄: %s\n' "$root"
}

guest_image() {
    case "$1" in
        debian) printf 'debian:13\n' ;;
        ubuntu) printf 'ubuntu:26.04\n' ;;
        alpine) printf 'alpine:3.23\n' ;;
        *)
            printf '錯誤：不支援的客體: %s\n' "$1" >&2
            return 1
            ;;
    esac
}

guest_name() {
    case "$1" in
        debian) printf 's12-debian13\n' ;;
        ubuntu) printf 's12-ubuntu2604\n' ;;
        alpine) printf 's12-alpine323\n' ;;
        *)
            printf '錯誤：不支援的客體: %s\n' "$1" >&2
            return 1
            ;;
    esac
}

map_host_arch() {
    case "$1" in
        x86_64|amd64) printf 'linux/amd64\n' ;;
        aarch64|arm64) printf 'linux/arm64\n' ;;
        *)
            printf '錯誤：不支援的架構: %s\n' "$1" >&2
            return 1
            ;;
    esac
}

detect_host_package_manager() {
    local manager

    for manager in apt-get dnf yum apk pacman zypper; do
        if command -v "$manager" >/dev/null 2>&1; then
            printf '%s\n' "$manager"
            return 0
        fi
    done
    return 1
}

run_with_privilege() {
    if (( EUID == 0 )); then
        "$@"
        return
    fi
    if command -v sudo >/dev/null 2>&1 && sudo -n true >/dev/null 2>&1; then
        sudo -n "$@"
        return
    fi
    printf '錯誤：安裝 PRoot 主機依賴需要 root 權限或可用的 sudo。\n' >&2
    return 1
}

install_host_dependencies() {
    local manager

    if ! manager="$(detect_host_package_manager)"; then
        printf '錯誤：找不到支援的套件管理器，無法安裝 proot 與 Python。\n' >&2
        return 1
    fi
    case "$manager" in
        apt-get)
            run_with_privilege apt-get update && \
                run_with_privilege apt-get install -y proot python3 python3-venv
            ;;
        dnf)
            run_with_privilege dnf install -y proot python3
            ;;
        yum)
            run_with_privilege yum install -y proot python3
            ;;
        apk)
            run_with_privilege apk add proot python3 py3-pip
            ;;
        pacman)
            run_with_privilege pacman -Sy --noconfirm --needed proot python
            ;;
        zypper)
            run_with_privilege zypper --non-interactive install proot python3
            ;;
    esac || {
        printf '錯誤：無法安裝 PRoot 主機依賴；請確認官方套件來源提供 proot。\n' >&2
        return 1
    }
}

python_is_supported() {
    local python_bin="${1:-python3}"

    "$python_bin" -c 'import sys; raise SystemExit(0 if sys.version_info >= (3, 9) else 1)' \
        >/dev/null 2>&1
}

find_supported_python() {
    local candidate python_path

    for candidate in python3 python3.15 python3.14 python3.13 python3.12 \
        python3.11 python3.10 python3.9; do
        python_path="$(command -v "$candidate" 2>/dev/null || true)"
        if [[ -n "$python_path" ]] && python_is_supported "$python_path"; then
            printf '%s\n' "$python_path"
            return 0
        fi
    done
    printf '錯誤：proot-distro %s 需要 Python 3.9 以上版本。\n' \
        "$PROOT_DISTRO_VERSION" >&2
    return 1
}

resolve_proot_distro_bin() {
    local bundled configured_bin

    configured_bin="${S12RYT_PROOT_DISTRO_BIN:-}"
    if [[ -n "$configured_bin" && -x "$configured_bin" ]]; then
        printf '%s\n' "$configured_bin"
        return
    fi
    if command -v proot-distro >/dev/null 2>&1; then
        command -v proot-distro
        return
    fi
    bundled="${S12RYT_PROOT_ROOT:-}/tool/bin/proot-distro"
    if [[ -x "$bundled" ]]; then
        printf '%s\n' "$bundled"
        return
    fi
    return 1
}

install_proot_distro_package() {
    local python_bin="$1"
    local tool_dir="${S12RYT_PROOT_ROOT}/tool"
    local pip_bin

    if [[ ! -x "${tool_dir}/bin/python3" ]]; then
        "$python_bin" -m venv "$tool_dir" || {
            printf '錯誤：無法建立 proot-distro Python 環境。\n' >&2
            return 1
        }
    fi
    pip_bin="${tool_dir}/bin/pip"
    [[ -x "$pip_bin" ]] || {
        printf '錯誤：proot-distro Python 環境缺少 pip。\n' >&2
        return 1
    }
    "$pip_bin" install --disable-pip-version-check \
        --index-url https://pypi.org/simple \
        "proot-distro==${PROOT_DISTRO_VERSION}" || {
        printf '錯誤：無法從 HTTPS PyPI 安裝 proot-distro。\n' >&2
        return 1
    }
    export S12RYT_PROOT_DISTRO_BIN="${tool_dir}/bin/proot-distro"
    [[ -x "$S12RYT_PROOT_DISTRO_BIN" ]] || {
        printf '錯誤：proot-distro 安裝完成但入口不存在。\n' >&2
        return 1
    }
}

setup_proot_tool() {
    local python_bin

    [[ "$(uname -s)" == "Linux" ]] || {
        printf '錯誤：PRoot 僅支援 Linux 主機。\n' >&2
        return 1
    }
    configure_proot_environment || return 1

    python_bin="$(find_supported_python 2>/dev/null || true)"
    if command -v proot >/dev/null 2>&1 && [[ -n "$python_bin" ]] && \
        resolve_proot_distro_bin >/dev/null 2>&1; then
        printf 'PRoot 管理工具已可使用。\n'
        return 0
    fi

    install_host_dependencies || return 1
    command -v proot >/dev/null 2>&1 || {
        printf '錯誤：套件安裝後仍找不到 proot。\n' >&2
        return 1
    }
    python_bin="$(find_supported_python)" || return 1
    install_proot_distro_package "$python_bin" || return 1
    printf 'PRoot 管理工具安裝完成。\n'
}

confirm_destructive_action() {
    local action="$1"
    local name="$2"
    local answer

    printf '確定要%s %s？資料將無法復原。 [y/N]: ' "$action" "$name"
    IFS= read -r answer || answer=""
    case "$answer" in
        y|Y|yes|YES) return 0 ;;
        *) return 1 ;;
    esac
}

print_proot_limitations() {
    printf '映像由 proot-distro 透過 HTTPS 取得，並驗證 OCI 逐層 SHA256 digest。\n'
    printf '提醒：PRoot 不是真正虛擬機，不提供真正 root、cgroup、systemd 或核心隔離。\n'
}

manage_guest() {
    local action="$1"
    local distro="${2:-}"
    local host_arch="${3:-$(uname -m)}"
    local image name platform proot_distro

    case "$action" in
        reinstall)
            name="$(guest_name "$distro")" || return 1
            if ! confirm_destructive_action "重裝" "$name"; then
                printf '已取消重裝。\n'
                return 0
            fi
            ;;
        remove)
            name="$(guest_name "$distro")" || return 1
            if ! confirm_destructive_action "移除" "$name"; then
                printf '已取消移除。\n'
                return 0
            fi
            ;;
    esac

    configure_proot_environment || return 1
    if ! proot_distro="$(resolve_proot_distro_bin)"; then
        printf '錯誤：找不到 proot-distro，請先執行 PRoot 腳本安裝。\n' >&2
        return 1
    fi

    case "$action" in
        install)
            image="$(guest_image "$distro")" || return 1
            name="$(guest_name "$distro")" || return 1
            platform="$(map_host_arch "$host_arch")" || return 1
            "$proot_distro" install "$image" --name "$name" --architecture "$platform" || return 1
            print_proot_limitations
            ;;
        login)
            name="$(guest_name "$distro")" || return 1
            print_proot_limitations
            "$proot_distro" login "$name"
            ;;
        list)
            "$proot_distro" list --quiet
            ;;
        reinstall)
            "$proot_distro" reset "$name"
            ;;
        remove)
            "$proot_distro" remove "$name"
            ;;
        *)
            printf '錯誤：不支援的 PRoot 操作: %s\n' "$action" >&2
            return 1
            ;;
    esac
}

select_guest() {
    local choice

    printf '選擇客體：1 Debian 13、2 Ubuntu 26.04 LTS、3 Alpine 3.23: '
    IFS= read -r choice || choice=""
    case "$choice" in
        1) printf 'debian\n' ;;
        2) printf 'ubuntu\n' ;;
        3) printf 'alpine\n' ;;
        *)
            printf '錯誤：無效的客體選項。\n' >&2
            return 1
            ;;
    esac
}

proot_manager_menu() {
    local choice distro

    while true; do
        cat <<'EOF'
----- PRoot 管理 -----
1. 安裝客體
2. 登入客體
3. 列出客體
4. 重裝客體
5. 移除客體
0. 返回
EOF
        printf '輸入選項: '
        IFS= read -r choice || return 0
        case "$choice" in
            0) return 0 ;;
            3) manage_guest list || true ;;
            1|2|4|5)
                distro="$(select_guest)" || continue
                case "$choice" in
                    1) manage_guest install "$distro" || true ;;
                    2) manage_guest login "$distro" || true ;;
                    4) manage_guest reinstall "$distro" || true ;;
                    5) manage_guest remove "$distro" || true ;;
                esac
                ;;
            *) printf '無效選項，請重新輸入。\n' >&2 ;;
        esac
    done
}

main() {
    case "${1:-manage}" in
        setup)
            setup_proot_tool
            ;;
        manage)
            setup_proot_tool && proot_manager_menu
            ;;
        *)
            printf '用法: %s [setup|manage]\n' "$0" >&2
            return 1
            ;;
    esac
}

if [[ "${BASH_SOURCE[0]}" == "$0" && "${S12RYT_PROOT_SOURCE_ONLY:-0}" != "1" ]]; then
    main "$@"
fi
