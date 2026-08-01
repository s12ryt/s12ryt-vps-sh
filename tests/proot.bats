#!/usr/bin/env bats

load test_helper

setup() {
    setup_sandbox
    setup_mock_bin
}

create_proot_distro_mock() {
    cat > "${MOCK_BIN}/proot-distro" <<'EOF'
#!/bin/bash
printf '%s|%s|proot-distro' "$XDG_DATA_HOME" "$XDG_CACHE_HOME" >> "$MOCK_LOG"
printf ' %s' "$@" >> "$MOCK_LOG"
printf '\n' >> "$MOCK_LOG"
if [[ "${1:-}" == "list" && "${2:-}" == "--quiet" ]]; then
    printf '%s\n' s12-debian13 s12-ubuntu2604 s12-alpine323
fi
EOF
    chmod +x "${MOCK_BIN}/proot-distro"
}

run_proot_function() {
    run env \
        HOME="$HOME" \
        XDG_DATA_HOME="$XDG_DATA_HOME" \
        XDG_CACHE_HOME="$XDG_CACHE_HOME" \
        PATH="$MOCK_BIN:/usr/bin:/bin" \
        MOCK_LOG="$MOCK_LOG" \
        S12RYT_PROOT_DISTRO_BIN="$MOCK_BIN/proot-distro" \
        S12RYT_PROOT_SOURCE_ONLY=1 \
        /bin/bash -c 'source "$1"; shift; "$@"' _ \
        "${PROJECT_ROOT}/install-proot.sh" "$@"
}

@test "PRoot 執行資料與快取都位於契約根目錄" {
    run_proot_function configure_proot_environment

    [ "$status" -eq 0 ]
    [[ "$output" == *"PRoot 根目錄: ${XDG_DATA_HOME}/s12ryt/proot"* ]]
    [ -d "${XDG_DATA_HOME}/s12ryt/proot/data" ]
    [ -d "${XDG_DATA_HOME}/s12ryt/proot/cache" ]
}

@test "固定客體映像與主機架構會映射至支援的 OCI 平台" {
    run env S12RYT_PROOT_SOURCE_ONLY=1 /bin/bash -c '
        source "$1"
        for distro in debian ubuntu alpine; do
            printf "%s|%s|%s\n" "$distro" "$(guest_image "$distro")" "$(guest_name "$distro")"
        done
        printf "x86=%s\n" "$(map_host_arch x86_64)"
        printf "arm=%s\n" "$(map_host_arch aarch64)"
        map_host_arch riscv64
    ' _ "${PROJECT_ROOT}/install-proot.sh"

    [ "$status" -ne 0 ]
    [[ "$output" == *"debian|debian:13|s12-debian13"* ]]
    [[ "$output" == *"ubuntu|ubuntu:26.04|s12-ubuntu2604"* ]]
    [[ "$output" == *"alpine|alpine:3.23|s12-alpine323"* ]]
    [[ "$output" == *"x86=linux/amd64"* ]]
    [[ "$output" == *"arm=linux/arm64"* ]]
    [[ "$output" == *"不支援的架構: riscv64"* ]]
}

@test "安裝三種客體時使用固定映像名稱與對應架構" {
    create_proot_distro_mock

    run_proot_function manage_guest install debian x86_64
    [ "$status" -eq 0 ]
    run_proot_function manage_guest install ubuntu aarch64
    [ "$status" -eq 0 ]
    run_proot_function manage_guest install alpine arm64
    [ "$status" -eq 0 ]

    expected_root="${XDG_DATA_HOME}/s12ryt/proot"
    grep -Fq "${expected_root}/data|${expected_root}/cache|proot-distro install debian:13 --name s12-debian13 --architecture linux/amd64" "$MOCK_LOG"
    grep -Fq "${expected_root}/data|${expected_root}/cache|proot-distro install ubuntu:26.04 --name s12-ubuntu2604 --architecture linux/arm64" "$MOCK_LOG"
    grep -Fq "${expected_root}/data|${expected_root}/cache|proot-distro install alpine:3.23 --name s12-alpine323 --architecture linux/arm64" "$MOCK_LOG"
    [[ "$output" == *"逐層 SHA256 digest"* ]]
    [[ "$output" == *"不是真正虛擬機"* ]]
}

@test "列表與登入交由 proot-distro 且破壞性操作必須確認" {
    create_proot_distro_mock

    run_proot_function manage_guest list
    [ "$status" -eq 0 ]
    [[ "$output" == *"s12-debian13"* ]]

    run_proot_function manage_guest login alpine
    [ "$status" -eq 0 ]

    : > "$MOCK_LOG"
    run env HOME="$HOME" XDG_DATA_HOME="$XDG_DATA_HOME" XDG_CACHE_HOME="$XDG_CACHE_HOME" \
        PATH="$MOCK_BIN:/usr/bin:/bin" MOCK_LOG="$MOCK_LOG" \
        S12RYT_PROOT_DISTRO_BIN="$MOCK_BIN/proot-distro" S12RYT_PROOT_SOURCE_ONLY=1 \
        /bin/bash -c 'source "$1"; printf "n\n" | manage_guest reinstall debian x86_64' \
        _ "${PROJECT_ROOT}/install-proot.sh"
    [ "$status" -eq 0 ]
    [ ! -s "$MOCK_LOG" ]
    [[ "$output" == *"已取消重裝"* ]]

    run env HOME="$HOME" XDG_DATA_HOME="$XDG_DATA_HOME" XDG_CACHE_HOME="$XDG_CACHE_HOME" \
        PATH="$MOCK_BIN:/usr/bin:/bin" MOCK_LOG="$MOCK_LOG" \
        S12RYT_PROOT_DISTRO_BIN="$MOCK_BIN/proot-distro" S12RYT_PROOT_SOURCE_ONLY=1 \
        /bin/bash -c 'source "$1"; printf "y\n" | manage_guest reinstall debian x86_64; printf "y\n" | manage_guest remove ubuntu' \
        _ "${PROJECT_ROOT}/install-proot.sh"
    [ "$status" -eq 0 ]
    grep -Fq 'proot-distro reset s12-debian13' "$MOCK_LOG"
    grep -Fq 'proot-distro remove s12-ubuntu2604' "$MOCK_LOG"
}

@test "安裝 s 時複製 PRoot helper 且選單 4 與 5 各自接線" {
    run_menu $'0\n'

    [ "$status" -eq 0 ]
    [ -x "$HOME/.local/share/s12ryt/install-proot.sh" ]

    run env HOME="$HOME" S12RYT_SOURCE_ONLY=1 /bin/bash -c '
        source "$1"
        install_launcher() { :; }
        prepare_proot_script() { printf "PREPARE_PROOT\n"; }
        run_proot_manager() { printf "MANAGE_PROOT\n"; }
        printf "4\n5\n0\n" | main
    ' _ "${PROJECT_ROOT}/s12ryt.sh"

    [ "$status" -eq 0 ]
    [[ "$output" == *"PREPARE_PROOT"* ]]
    [[ "$output" == *"MANAGE_PROOT"* ]]
}

@test "下載到語法無效的 PRoot helper 時保留既有版本" {
    target="${TEST_ROOT}/installed/install-proot.sh"
    mkdir -p "${target%/*}"
    printf '%s\n' '#!/usr/bin/env bash' 'printf old-helper' > "$target"
    chmod +x "$target"

    cat > "${MOCK_BIN}/curl" <<'EOF'
#!/bin/bash
output=""
while (($#)); do
    if [[ "$1" == "-o" ]]; then
        output="$2"
        shift 2
    else
        shift
    fi
done
printf '%s\n' '#!/usr/bin/env bash' 'if broken' > "$output"
EOF
    chmod +x "${MOCK_BIN}/curl"

    run env PATH="$MOCK_BIN:/usr/bin:/bin" S12RYT_SOURCE_ONLY=1 \
        S12RYT_PROOT_HELPER_SOURCE="${TEST_ROOT}/missing-helper.sh" \
        S12RYT_PROOT_HELPER_PATH="$target" \
        /bin/bash -c 'source "$1"; ensure_proot_helper' _ "${PROJECT_ROOT}/s12ryt.sh"

    [ "$status" -ne 0 ]
    [[ "$output" == *"PRoot 腳本語法驗證失敗"* ]]
    run "$target"
    [ "$status" -eq 0 ]
    [ "$output" = "old-helper" ]
}
