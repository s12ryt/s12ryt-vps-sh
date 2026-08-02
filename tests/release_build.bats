#!/usr/bin/env bats

load test_helper

setup() {
    setup_sandbox
    setup_mock_bin
    export PATH="${MOCK_BIN}:/usr/bin:/bin"
    export S12RYT_RELEASE_BUILD_SOURCE="$PROJECT_ROOT"

    cat > "${MOCK_BIN}/go" <<'EOF'
#!/bin/bash
printf 'go GOOS=%s GOARCH=%s CGO_ENABLED=%s' "${GOOS:-}" "${GOARCH:-}" "${CGO_ENABLED:-}" >> "$MOCK_LOG"
printf ' %s' "$@" >> "$MOCK_LOG"
printf '\n' >> "$MOCK_LOG"

if [[ "${MOCK_GO_FAIL_ARCH:-}" == "${GOARCH:-}" ]]; then
    exit 42
fi

output=""
while (($# > 0)); do
    if [[ "$1" == "-o" && $# -ge 2 ]]; then
        output="$2"
        shift 2
        continue
    fi
    shift
done

[[ -n "$output" ]] || exit 43
mkdir -p "$(dirname "$output")"
printf 'linux-%s\n' "$GOARCH" > "$output"
chmod 0755 "$output"
EOF
    chmod +x "${MOCK_BIN}/go"
}

run_release_builder() {
    local destination="$1"

    run /bin/bash -c 'source "$1"; build_release_assets "$2"' _ \
        "${PROJECT_ROOT}/scripts/build-release.sh" "$destination"
}

@test "release builder 交叉編譯 amd64 arm64 並產生可驗證的 SHA256SUMS" {
    output="${TEST_ROOT}/release"

    run_release_builder "$output"

    [ "$status" -eq 0 ]
    [ -x "${output}/s12ryt-ipv6-linux-amd64" ]
    [ -x "${output}/s12ryt-ipv6-linux-arm64" ]
    [ -f "${output}/SHA256SUMS" ]
    [ "$(wc -l < "${output}/SHA256SUMS")" -eq 2 ]

    run /bin/bash -c 'cd "$1" && sha256sum -c SHA256SUMS' _ "$output"
    [ "$status" -eq 0 ]
    [[ "$output" == *"s12ryt-ipv6-linux-amd64: OK"* ]]
    [[ "$output" == *"s12ryt-ipv6-linux-arm64: OK"* ]]

    grep -Fq 'go GOOS=linux GOARCH=amd64 CGO_ENABLED=0 build -trimpath -ldflags -s -w -o' "$MOCK_LOG"
    grep -Fq 'go GOOS=linux GOARCH=arm64 CGO_ENABLED=0 build -trimpath -ldflags -s -w -o' "$MOCK_LOG"
    [ "$(grep -Fc ' ./cmd/s12ryt-ipv6' "$MOCK_LOG")" -eq 2 ]
}

@test "任一架構建置失敗時不發布部分資產" {
    output="${TEST_ROOT}/release"
    export MOCK_GO_FAIL_ARCH=arm64

    run_release_builder "$output"

    [ "$status" -ne 0 ]
    [ ! -e "$output" ]
    [ -z "$(find "$TEST_ROOT" -maxdepth 1 -name '.s12ryt-release.*' -print -quit)" ]
}

@test "release builder 拒絕覆寫既有輸出目錄" {
    output="${TEST_ROOT}/release"
    mkdir -p "$output"
    printf 'keep\n' > "${output}/sentinel"

    run_release_builder "$output"

    [ "$status" -ne 0 ]
    [ "$(cat "${output}/sentinel")" = "keep" ]
    [ ! -s "$MOCK_LOG" ]
}
