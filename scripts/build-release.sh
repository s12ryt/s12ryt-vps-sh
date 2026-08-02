#!/usr/bin/env bash
# Copyright (C) 2026 s12ryt
# SPDX-License-Identifier: GPL-3.0-only

set -u

release_build_source_root() {
    local source_root="${S12RYT_RELEASE_BUILD_SOURCE:-}"
    local script_directory=""

    if [[ -z "$source_root" ]]; then
        script_directory="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)" || return 1
        source_root="${script_directory}/.."
    fi

    cd -- "$source_root" && pwd -P
}

require_release_build_command() {
    local command_name="$1"

    if ! command -v "$command_name" >/dev/null 2>&1; then
        printf '錯誤：缺少發行資產建置命令 %s。\n' "$command_name" >&2
        return 1
    fi
}

build_release_assets() {
    local destination="${1:-}"
    local source_root=""
    local destination_parent=""
    local staging=""
    local architecture=""
    local asset=""
    local command_name=""

    if [[ $# -ne 1 || -z "$destination" ]]; then
        printf '錯誤：必須提供發行資產輸出目錄。\n' >&2
        return 1
    fi
    if [[ -e "$destination" ]]; then
        printf '錯誤：發行資產輸出目錄已存在。\n' >&2
        return 1
    fi

    for command_name in go sha256sum mktemp mkdir mv rm chmod dirname; do
        require_release_build_command "$command_name" || return 1
    done

    source_root="$(release_build_source_root)" || {
        printf '錯誤：無法解析發行資產原始碼目錄。\n' >&2
        return 1
    }
    if [[ ! -f "${source_root}/go.mod" || ! -d "${source_root}/cmd/s12ryt-ipv6" ]]; then
        printf '錯誤：發行資產原始碼目錄不完整。\n' >&2
        return 1
    fi

    destination_parent="$(dirname -- "$destination")"
    mkdir -p -- "$destination_parent" || {
        printf '錯誤：無法建立發行資產父目錄。\n' >&2
        return 1
    }
    staging="$(mktemp -d "${destination_parent}/.s12ryt-release.XXXXXX")" || {
        printf '錯誤：無法建立發行資產暫存目錄。\n' >&2
        return 1
    }

    for architecture in amd64 arm64; do
        asset="${staging}/s12ryt-ipv6-linux-${architecture}"
        if ! (
            cd -- "$source_root" &&
                CGO_ENABLED=0 GOOS=linux GOARCH="$architecture" \
                    go build -trimpath -ldflags '-s -w' -o "$asset" ./cmd/s12ryt-ipv6
        ); then
            rm -rf -- "${staging:?}"
            printf '錯誤：%s 發行資產建置失敗。\n' "$architecture" >&2
            return 1
        fi
        if ! chmod 0755 "$asset"; then
            rm -rf -- "${staging:?}"
            printf '錯誤：無法設定 %s 發行資產權限。\n' "$architecture" >&2
            return 1
        fi
    done

    if ! (
        cd -- "$staging" &&
            sha256sum s12ryt-ipv6-linux-amd64 s12ryt-ipv6-linux-arm64 > SHA256SUMS &&
            sha256sum -c SHA256SUMS >/dev/null
    ); then
        rm -rf -- "${staging:?}"
        printf '錯誤：發行資產 SHA256 驗證失敗。\n' >&2
        return 1
    fi

    if ! mv -- "$staging" "$destination"; then
        rm -rf -- "${staging:?}"
        printf '錯誤：無法發布發行資產。\n' >&2
        return 1
    fi

    printf '發行資產已建立：%s\n' "$destination"
}

main() {
    case "${1:-}" in
        build)
            shift
            build_release_assets "$@"
            ;;
        *)
            printf '用法：%s build OUTPUT_DIRECTORY\n' "$0" >&2
            return 1
            ;;
    esac
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
    main "$@"
fi
