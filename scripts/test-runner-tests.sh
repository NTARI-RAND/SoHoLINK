#!/usr/bin/env bash
# Exercise argument boundaries and failure propagation without compiling Go.
set -euo pipefail

runner="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)/test.sh"
test_dir=$(mktemp -d)
trap 'rm -rf "$test_dir"' EXIT

cat > "$test_dir/go" <<'EOF'
#!/usr/bin/env bash
printf '%s\n' "$@" > "$TEST_ARGS"
exit "$FAKE_GO_EXIT"
EOF
chmod +x "$test_dir/go"

run_case() {
    local timeout=$1 go_exit=$2 expected_exit=$3 actual_exit
    local mode=${4:-short}
    local -a expected_flags=(-short)
    if [[ "$mode" == defaults ]]; then
        expected_flags=(-race -coverprofile=coverage.out)
    fi
    rm -f "$test_dir/args"
    if (
        cd -- "$test_dir"
        unset SHORT_MODE RACE_DETECTION COVERAGE
        export PATH="$test_dir:$PATH" TEST_ARGS="$test_dir/args" \
            FAKE_GO_EXIT="$go_exit" TEST_TIMEOUT="$timeout"
        if [[ "$mode" == defaults ]]; then
            bash "$runner"
        else
            bash "$runner" --short --no-race --no-coverage
        fi
    ) > "$test_dir/output" 2>&1; then
        actual_exit=0
    else
        actual_exit=$?
    fi
    if [[ "$actual_exit" != "$expected_exit" ]]; then
        cat "$test_dir/output"
        printf 'runner exit: got %s, want %s\n' "$actual_exit" "$expected_exit" >&2
        return 1
    fi
    printf '%s\n' test -v -timeout "$timeout" "${expected_flags[@]}" ./internal/... > "$test_dir/expected"
    diff -u "$test_dir/expected" "$test_dir/args"
}

run_case 120s 0 0
run_case '*' 0 0
run_case 120s 1 1
run_case 120s 0 0 defaults
printf 'test runner: 4 cases passed\n'
