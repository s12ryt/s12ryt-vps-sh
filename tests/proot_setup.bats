#!/usr/bin/env bats

load test_helper

setup() {
    setup_sandbox
    setup_mock_bin
}

@test "PRoot 主機依賴使用非互動 sudo 執行實際命令" {
    cat > "${MOCK_BIN}/sudo" <<'EOF'
#!/bin/bash
printf 'sudo %s\n' "$*" >> "$MOCK_LOG"
if [[ "${1:-}" == "-n" && "${2:-}" == "true" ]]; then
    exit 0
fi
if [[ "${1:-}" == "-n" ]]; then
    shift
fi
"$@"
EOF
    cat > "${MOCK_BIN}/apt-get" <<'EOF'
#!/bin/bash
printf 'apt-get %s\n' "$*" >> "$MOCK_LOG"
EOF
    chmod +x "${MOCK_BIN}/sudo" "${MOCK_BIN}/apt-get"

    run env PATH="$MOCK_BIN:/usr/bin:/bin" MOCK_LOG="$MOCK_LOG" \
        S12RYT_PROOT_SOURCE_ONLY=1 /bin/bash -c \
        'source "$1"; run_with_privilege apt-get install -y proot' \
        _ "${PROJECT_ROOT}/install-proot.sh"

    [ "$status" -eq 0 ]
    grep -Fxq 'sudo -n true' "$MOCK_LOG"
    grep -Fxq 'sudo -n apt-get install -y proot' "$MOCK_LOG"
}

@test "PRoot 會略過過舊 python3 並選擇可用的 Python 3.9 以上版本" {
    cat > "${MOCK_BIN}/python3" <<'EOF'
#!/bin/bash
exit 1
EOF
    cat > "${MOCK_BIN}/python3.11" <<'EOF'
#!/bin/bash
exit 0
EOF
    chmod +x "${MOCK_BIN}/python3" "${MOCK_BIN}/python3.11"

    run env PATH="$MOCK_BIN:/usr/bin:/bin" S12RYT_PROOT_SOURCE_ONLY=1 \
        /bin/bash -c 'source "$1"; find_supported_python' \
        _ "${PROJECT_ROOT}/install-proot.sh"

    [ "$status" -eq 0 ]
    [ "$output" = "${MOCK_BIN}/python3.11" ]
}

@test "proot-distro 使用隔離 venv 從 HTTPS PyPI 安裝釘選版本" {
    cat > "${MOCK_BIN}/python3.11" <<'EOF'
#!/bin/bash
printf 'python3.11 %s\n' "$*" >> "$MOCK_LOG"
if [[ "${1:-}" == "-m" && "${2:-}" == "venv" ]]; then
    mkdir -p "$3/bin"
    cat > "$3/bin/pip" <<'INNER'
#!/bin/bash
printf 'pip %s\n' "$*" >> "$MOCK_LOG"
touch "${0%/pip}/proot-distro"
chmod +x "${0%/pip}/proot-distro"
INNER
    chmod +x "$3/bin/pip"
fi
EOF
    chmod +x "${MOCK_BIN}/python3.11"

    run env HOME="$HOME" XDG_DATA_HOME="$XDG_DATA_HOME" \
        XDG_CACHE_HOME="$XDG_CACHE_HOME" PATH="$MOCK_BIN:/usr/bin:/bin" \
        MOCK_LOG="$MOCK_LOG" S12RYT_PROOT_SOURCE_ONLY=1 /bin/bash -c \
        'source "$1"; configure_proot_environment >/dev/null; install_proot_distro_package "$2"' \
        _ "${PROJECT_ROOT}/install-proot.sh" "${MOCK_BIN}/python3.11"

    [ "$status" -eq 0 ]
    tool_dir="${XDG_DATA_HOME}/s12ryt/proot/tool"
    grep -Fxq "python3.11 -m venv ${tool_dir}" "$MOCK_LOG"
    grep -Fxq 'pip install --disable-pip-version-check --index-url https://pypi.org/simple proot-distro==5.5.0' "$MOCK_LOG"
    [ -x "${tool_dir}/bin/proot-distro" ]
}

@test "套件來源無法提供 PRoot 主機依賴時會明確失敗" {
    run env S12RYT_PROOT_SOURCE_ONLY=1 /bin/bash -c '
        source "$1"
        detect_host_package_manager() { printf "dnf\n"; }
        run_with_privilege() { return 23; }
        install_host_dependencies
    ' _ "${PROJECT_ROOT}/install-proot.sh"

    [ "$status" -ne 0 ]
    [[ "$output" == *"無法安裝 PRoot 主機依賴"* ]]
    [[ "$output" == *"請確認官方套件來源提供 proot"* ]]
}
