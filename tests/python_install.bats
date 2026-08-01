#!/usr/bin/env bats

load test_helper

setup() {
    setup_sandbox
    setup_mock_bin
    setup_runtime_path
    export PYTHON_ROOT="${XDG_DATA_HOME}/s12ryt/python"
    export PYTHON_BIN_DIR="${HOME}/.local/bin"
    export S12RYT_UV_FIXTURE="${TEST_ROOT}/uv-fixture"
    create_uv_fixture
}

setup_runtime_path() {
    local command_path command_name

    for command_name in awk bash cat chmod cp cut dirname ln mkdir mktemp mv readlink rm touch; do
        command_path="$(command -v "$command_name")"
        ln -s "$command_path" "${MOCK_BIN}/${command_name}"
    done
    export PATH="$MOCK_BIN"
}

write_python_mock() {
    local target="$1"
    local minor="$2"
    local pip_state="${3:-ready}"

    mkdir -p "$(dirname "$target")"
    cat > "$target" <<EOF
#!/bin/bash
printf 'python-${minor} %s\n' "\$*" >> "\$MOCK_LOG"
case "\$*" in
    --version)
        printf 'Python ${minor}.9\n'
        ;;
    '-m pip --version')
        [[ -f '${pip_state}' || '${pip_state}' == 'ready' ]] || exit 1
        printf 'pip 26.1 from managed (${minor})\n'
        ;;
    '-m ensurepip'* )
        : > '${pip_state}'
        ;;
    *)
        exit 1
        ;;
esac
EOF
    chmod +x "$target"
}

create_uv_fixture() {
    cat > "$S12RYT_UV_FIXTURE" <<'EOF'
#!/bin/bash
printf 'uv-env install=%s bin=%s\n' "${UV_PYTHON_INSTALL_DIR:-}" "${UV_PYTHON_BIN_DIR:-}" >> "$MOCK_LOG"
printf 'uv %s\n' "$*" >> "$MOCK_LOG"
write_mock_python() {
    target="$1"
    minor="$2"
    mkdir -p "$(dirname "$target")"
    cat > "$target" <<SCRIPT
#!/bin/bash
printf 'Python ${minor}.9\\n'
if [[ "\$*" == '-m pip --version' ]]; then
    printf 'pip 26.1 from uv (${minor})\\n'
elif [[ "\$*" != '--version' ]]; then
    exit 1
fi
SCRIPT
    chmod +x "$target"
}
if [[ "$1" == "--version" ]]; then
    printf 'uv 0.12.1\n'
elif [[ "$1" == "python" && "$2" == "install" ]]; then
    [[ "${S12RYT_UV_MODE:-}" != "python-fail" ]] || exit 8
    minor="$3"
    mkdir -p "$UV_PYTHON_INSTALL_DIR/cpython-${minor}/bin" "$UV_PYTHON_BIN_DIR"
    write_mock_python "$UV_PYTHON_INSTALL_DIR/cpython-${minor}/bin/python${minor}" "$minor"
    ln -sf "$UV_PYTHON_INSTALL_DIR/cpython-${minor}/bin/python${minor}" \
        "$UV_PYTHON_BIN_DIR/python${minor}"
elif [[ "$1" == "venv" ]]; then
    [[ "${S12RYT_UV_MODE:-}" != "venv-fail" ]] || exit 9
    shift
    minor=""
    destination=""
    while (($#)); do
        case "$1" in
            --python)
                selector="$2"
                minor="${selector##*python}"
                [[ "$minor" =~ ^3\.[0-9]+$ ]] || minor="$("$selector" --version | awk '{print $2}' | cut -d. -f1,2)"
                shift 2
                ;;
            --seed)
                shift
                ;;
            *)
                destination="$1"
                shift
                ;;
        esac
    done
    write_mock_python "$destination/bin/python" "$minor"
else
    exit 2
fi
EOF
    chmod +x "$S12RYT_UV_FIXTURE"
}

create_uv_curl_mock() {
    cat > "${MOCK_BIN}/curl" <<'EOF'
#!/bin/bash
printf 'curl %s\n' "$*" >> "$MOCK_LOG"
output=""
while (($#)); do
    if [[ "$1" == "-o" ]]; then
        output="$2"
        shift 2
    else
        shift
    fi
done
case "${S12RYT_CURL_MODE:-success}" in
    download-fail)
        exit 22
        ;;
    syntax-fail)
        printf 'if broken\n' > "$output"
        ;;
    *)
        cat > "$output" <<'INSTALLER'
#!/bin/bash
[[ "${S12RYT_CURL_MODE:-success}" != "installer-fail" ]] || exit 7
printf 'uv-installer dir=%s no-path=%s\n' "$UV_INSTALL_DIR" "$UV_NO_MODIFY_PATH" >> "$MOCK_LOG"
mkdir -p "$UV_INSTALL_DIR"
cp "$S12RYT_UV_FIXTURE" "$UV_INSTALL_DIR/uv"
chmod +x "$UV_INSTALL_DIR/uv"
INSTALLER
        ;;
esac
EOF
    chmod +x "${MOCK_BIN}/curl"
}

create_python_manager_mock() {
    create_command_mock "${1:-apt-get}"
}

run_python_installer() {
    local input="$1"
    shift

    run env HOME="$HOME" XDG_DATA_HOME="$XDG_DATA_HOME" XDG_CACHE_HOME="$XDG_CACHE_HOME" \
        PATH="$PATH" MOCK_LOG="$MOCK_LOG" S12RYT_UV_FIXTURE="$S12RYT_UV_FIXTURE" \
        S12RYT_SOURCE_ONLY=1 S12RYT_EFFECTIVE_UID="${S12RYT_EFFECTIVE_UID:-1000}" \
        "$@" /bin/bash -c 'source "$1"; printf "%s" "$2" | install_python' \
        _ "${PROJECT_ROOT}/s12ryt.sh" "$input"
}

@test "Python 版本與私有 uv 路徑符合契約" {
    run env HOME="$HOME" XDG_DATA_HOME="$XDG_DATA_HOME" S12RYT_SOURCE_ONLY=1 \
        S12RYT_EFFECTIVE_UID=1000 /bin/bash -c '
        source "$1"
        for version in 3.10 3.11 3.12 3.13 3.14; do
            validate_python_minor "$version" || exit 1
        done
        ! validate_python_minor 3.9
        printf "%s\n" "$(python_runtime_root)" "$(python_uv_bin)" \
            "$(python_version_bin_dir)" "$(python_venv_path 3.12)"
    ' _ "${PROJECT_ROOT}/s12ryt.sh"

    [ "$status" -eq 0 ]
    [[ "$output" == *"${PYTHON_ROOT}"* ]]
    [[ "$output" == *"${PYTHON_ROOT}/uv/uv"* ]]
    [[ "$output" == *"${HOME}/.local/bin"* ]]
    [[ "$output" == *"${PYTHON_ROOT}/venvs/3.12"* ]]

    run env HOME="$HOME" XDG_DATA_HOME="$XDG_DATA_HOME" S12RYT_SOURCE_ONLY=1 \
        S12RYT_EFFECTIVE_UID=0 /bin/bash -c 'source "$1"; python_version_bin_dir' \
        _ "${PROJECT_ROOT}/s12ryt.sh"
    [ "$status" -eq 0 ]
    [ "$output" = "/usr/local/bin" ]
}

@test "取消 Python 安裝或缺少非互動管理權限時不下載" {
    create_python_manager_mock apt-get
    create_uv_curl_mock

    run_python_installer $'3.10\nn\n'
    [ "$status" -eq 0 ]
    [[ "$output" == *"套件管理器: apt-get"* ]]
    [[ "$output" == *"uv 0.12.1"* ]]
    [[ "$output" == *"已取消 Python 安裝"* ]]
    [ ! -s "$MOCK_LOG" ]

    run_python_installer $'3.10\ny\n'
    [ "$status" -ne 0 ]
    [[ "$output" == *"需要 root 權限或可用的 sudo"* ]]
    [ ! -s "$MOCK_LOG" ]
}

@test "uv 為五個 Python minor 建立版本命令 direct pip 與固定 seeded venv" {
    local minor python_command venv_python

    create_python_manager_mock apt-get
    create_uv_curl_mock
    create_command_mock sudo
    for minor in 3.10 3.11 3.12 3.13 3.14; do
        rm -rf "$PYTHON_ROOT" "$PYTHON_BIN_DIR"
        : > "$MOCK_LOG"

        run_python_installer "$(printf '%s\ny\n' "$minor")"

        [ "$status" -eq 0 ]
        python_command="${PYTHON_BIN_DIR}/python${minor}"
        venv_python="${PYTHON_ROOT}/venvs/${minor}/bin/python"
        [ -x "$PYTHON_ROOT/uv/uv" ]
        [ -x "$python_command" ]
        [ -x "$venv_python" ]
        [ ! -e "$PYTHON_BIN_DIR/python" ]
        [ ! -e "$PYTHON_BIN_DIR/python3" ]
        [ ! -e "$PYTHON_BIN_DIR/pip" ]
        [ ! -e "$PYTHON_BIN_DIR/pip3" ]
        run "$python_command" -m pip --version
        [ "$status" -eq 0 ]
        run "$venv_python" -m pip --version
        [ "$status" -eq 0 ]
        [[ "$(cat "$MOCK_LOG")" == *"curl -fsSL --connect-timeout 5 --max-time 30 https://releases.astral.sh/github/uv/releases/download/0.12.1/uv-installer.sh"* ]]
        [[ "$(cat "$MOCK_LOG")" == *"uv-installer dir=${PYTHON_ROOT}/uv no-path=1"* ]]
        [[ "$(cat "$MOCK_LOG")" == *"uv-env install=${PYTHON_ROOT}/versions bin=${PYTHON_BIN_DIR}"* ]]
        [[ "$(cat "$MOCK_LOG")" == *"uv python install ${minor}"* ]]
        [[ "$(cat "$MOCK_LOG")" == *"uv venv --python ${minor} --seed ${PYTHON_ROOT}/venvs/${minor}"* ]]
        [[ "$output" == *"Python ${minor}.9"* ]]
        [[ "$output" == *"pip 26.1"* ]]
    done
}

@test "既有 Python direct pip 與固定 venv 完整時直接跳過" {
    write_python_mock "${PYTHON_BIN_DIR}/python3.12" 3.12 ready
    write_python_mock "${PYTHON_ROOT}/venvs/3.12/bin/python" 3.12 ready

    run_python_installer $'3.12\n'

    [ "$status" -eq 0 ]
    [[ "$output" == *"Python 3.12.9"* ]]
    [[ "$output" == *"已完整安裝"* ]]
    [[ "$(cat "$MOCK_LOG")" != *"curl "* ]]
    [[ "$(cat "$MOCK_LOG")" != *"uv "* ]]
}

@test "拒絕補齊既有 Python 時不修改任何內容" {
    local existing="${PYTHON_BIN_DIR}/python3.11"

    write_python_mock "$existing" 3.11 "${TEST_ROOT}/missing-pip"
    create_uv_curl_mock

    run_python_installer $'3.11\nn\n'

    [ "$status" -eq 0 ]
    [[ "$output" == *"缺少"* ]]
    [[ "$output" == *"已取消補齊"* ]]
    [ ! -e "$PYTHON_ROOT/uv/uv" ]
    [ ! -e "$PYTHON_ROOT/venvs/3.11" ]
    [[ "$(cat "$MOCK_LOG")" != *"ensurepip"* ]]
}

@test "非 uv 的既有 Python 只補固定 venv且不修改 direct pip" {
    local system_python="${MOCK_BIN}/python3.11"

    write_python_mock "$system_python" 3.11 "${TEST_ROOT}/system-pip-missing"
    create_python_manager_mock apt-get
    create_uv_curl_mock
    create_command_mock sudo

    run_python_installer $'3.11\ny\n'

    [ "$status" -eq 0 ]
    [ -x "$PYTHON_ROOT/venvs/3.11/bin/python" ]
    [ ! -e "${TEST_ROOT}/system-pip-missing" ]
    [[ "$(cat "$MOCK_LOG")" != *"uv python install"* ]]
    [[ "$(cat "$MOCK_LOG")" != *"ensurepip"* ]]
    [[ "$(cat "$MOCK_LOG")" == *"uv venv --python ${system_python} --seed ${PYTHON_ROOT}/venvs/3.11"* ]]
    [[ "$output" == *"直接 pip 仍由系統安裝狀態決定"* ]]
}

@test "uv 受管 Python 缺 direct pip 時只對受管版本執行 ensurepip" {
    local managed_python="${PYTHON_ROOT}/versions/cpython-3.13/bin/python3.13"
    local pip_state="${TEST_ROOT}/managed-pip"

    write_python_mock "$managed_python" 3.13 "$pip_state"
    mkdir -p "$PYTHON_BIN_DIR" "$PYTHON_ROOT/uv"
    ln -s "$managed_python" "$PYTHON_BIN_DIR/python3.13"
    cp "$S12RYT_UV_FIXTURE" "$PYTHON_ROOT/uv/uv"
    chmod +x "$PYTHON_ROOT/uv/uv"
    create_python_manager_mock apt-get
    create_command_mock sudo

    run_python_installer $'3.13\ny\n'

    [ "$status" -eq 0 ]
    [ -e "$pip_state" ]
    [[ "$(cat "$MOCK_LOG")" == *"python-3.13 -m ensurepip"* ]]
    [ -x "$PYTHON_ROOT/venvs/3.13/bin/python" ]
}

@test "uv 下載語法安裝 Python 或 venv 失敗時不得誤報成功" {
    local mode

    create_python_manager_mock apt-get
    create_uv_curl_mock
    export S12RYT_EFFECTIVE_UID=0
    for mode in download-fail syntax-fail installer-fail python-fail venv-fail; do
        rm -rf "$PYTHON_ROOT" "$PYTHON_BIN_DIR"
        : > "$MOCK_LOG"

        run_python_installer $'3.14\ny\n' S12RYT_CURL_MODE="$mode" S12RYT_UV_MODE="$mode"

        [ "$status" -ne 0 ]
        [[ "$output" != *"Python 3.14 安裝完成"* ]]
    done
}
