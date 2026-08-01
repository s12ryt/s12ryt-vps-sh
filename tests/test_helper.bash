PROJECT_ROOT="$(cd "${BATS_TEST_DIRNAME}/.." && pwd)"

setup_sandbox() {
    export TEST_ROOT="${BATS_TEST_TMPDIR}/s12ryt-${BATS_TEST_NUMBER}"
    export HOME="${TEST_ROOT}/home"
    export XDG_DATA_HOME="${TEST_ROOT}/xdg-data"
    export XDG_CACHE_HOME="${TEST_ROOT}/xdg-cache"
    export PATH="/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"
    mkdir -p "$HOME"
}

run_menu() {
    local input="$1"
    run bash -c 'printf "%s" "$1" | bash "$2"' _ "$input" "${PROJECT_ROOT}/s12ryt.sh"
}
