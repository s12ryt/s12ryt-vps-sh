PROJECT_ROOT="$(cd "${BATS_TEST_DIRNAME}/.." && pwd)"

setup_sandbox() {
    export TEST_ROOT="${BATS_TEST_TMPDIR}/s12ryt-${BATS_TEST_NUMBER}"
    export HOME="${TEST_ROOT}/home"
    export XDG_DATA_HOME="${TEST_ROOT}/xdg-data"
    export XDG_CACHE_HOME="${TEST_ROOT}/xdg-cache"
    export PATH="/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"
    mkdir -p "$HOME"
}

setup_mock_bin() {
    export MOCK_BIN="${TEST_ROOT}/mock-bin"
    export MOCK_LOG="${TEST_ROOT}/commands.log"
    mkdir -p "$MOCK_BIN"
    : > "$MOCK_LOG"
}

create_command_mock() {
    local command_name="$1"

    cat > "${MOCK_BIN}/${command_name}" <<'EOF'
#!/bin/bash
printf '%s' "${0##*/}" >> "$MOCK_LOG"
printf ' %s' "$@" >> "$MOCK_LOG"
printf '\n' >> "$MOCK_LOG"
if [[ "${0##*/}" == "sudo" ]]; then
    if [[ "${1:-}" == "-n" && "${2:-}" == "true" ]]; then
        exit 0
    fi
    if [[ "${1:-}" == "-n" ]]; then
        shift
    fi
    exec "$@"
fi
EOF
    chmod +x "${MOCK_BIN}/${command_name}"
}

run_menu() {
    local input="$1"
    run bash -c 'printf "%s" "$1" | bash "$2"' _ "$input" "${PROJECT_ROOT}/s12ryt.sh"
}
