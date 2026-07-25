#!/usr/bin/env bash

set -u

ROOT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
CHECKER_SOURCE="$ROOT_DIR/scripts/check-repository-hygiene.sh"
TEST_TMP_BASE=${TMPDIR:-/tmp}
TEMP_ROOT=$(mktemp -d "$TEST_TMP_BASE/denova-repository-hygiene.XXXXXX") || exit 1

passed=0
failed=0
skipped=0

now_milliseconds() {
  perl -MTime::HiRes=time -e 'printf "%.0f\n", time * 1000'
}

cleanup() {
  case "$TEMP_ROOT" in
    "$TEST_TMP_BASE"/denova-repository-hygiene.*)
      if [ -d "$TEMP_ROOT" ]; then
        rm -rf -- "$TEMP_ROOT"
      fi
      ;;
    *)
      printf 'Refusing to clean unexpected test path: %s\n' "$TEMP_ROOT" >&2
      exit 1
      ;;
  esac
}
trap cleanup EXIT HUP INT TERM

# Keep one-time Git initialization overhead out of the per-case budget.
mkdir -p "$TEMP_ROOT/warmup"
git -C "$TEMP_ROOT/warmup" init -q

new_repo() {
  repo_name=$1
  repo_path="$TEMP_ROOT/$repo_name"
  mkdir -p "$repo_path/scripts"
  git -C "$repo_path" init -q
  git -C "$repo_path" config user.email "repository-hygiene@example.invalid"
  git -C "$repo_path" config user.name "Repository Hygiene Test"

  if [ ! -x "$CHECKER_SOURCE" ]; then
    printf 'Missing executable public command: %s\n' "$CHECKER_SOURCE" >&2
    return 1
  fi

  cp "$CHECKER_SOURCE" "$repo_path/scripts/check-repository-hygiene.sh"
  chmod +x "$repo_path/scripts/check-repository-hygiene.sh"
  printf '%s\n' "$repo_path"
}

run_checker() {
  checker_workdir=$1
  shift
  output=$(cd "$checker_workdir" && "$@" 2>&1)
  status=$?
}

case_allows_product_and_normative_files() {
  repo=$(new_repo allows-product-and-normative-files) || return 1
  mkdir -p "$repo/internal/example" "$repo/docs/project-design/adr"
  printf 'package example\n' >"$repo/internal/example/example.go"
  printf '# ADR-001: Keep normative decisions in Git\n' >"$repo/docs/project-design/adr/ADR-001.md"
  git -C "$repo" add .

  run_checker "$repo" ./scripts/check-repository-hygiene.sh
  if [ "$status" -ne 0 ]; then
    printf 'expected exit 0, got %s\n%s\n' "$status" "$output" >&2
    return 1
  fi
}

case_rejects_forbidden_path() {
  forbidden_path=$1
  repo=$(new_repo "forbidden-$failed-$passed") || return 1
  mkdir -p "$(dirname -- "$repo/$forbidden_path")"
  printf 'raw evidence\n' >"$repo/$forbidden_path"
  git -C "$repo" add .

  run_checker "$repo" ./scripts/check-repository-hygiene.sh
  if [ "$status" -eq 0 ]; then
    printf 'expected nonzero exit for %s\n%s\n' "$forbidden_path" "$output" >&2
    return 1
  fi
  case "$output" in
    *"$forbidden_path"*"forbidden evidence"*"原始证据"*) ;;
    *)
      printf 'expected path and bilingual forbidden-evidence reason for %s\n%s\n' "$forbidden_path" "$output" >&2
      return 1
      ;;
  esac
}

case_allows_path() {
  allowed_path=$1
  repo=$(new_repo "allowed-$failed-$passed") || return 1
  mkdir -p "$(dirname -- "$repo/$allowed_path")"
  printf 'normative material\n' >"$repo/$allowed_path"
  git -C "$repo" add .

  run_checker "$repo" ./scripts/check-repository-hygiene.sh
  if [ "$status" -ne 0 ]; then
    printf 'expected exit 0 for %s, got %s\n%s\n' "$allowed_path" "$status" "$output" >&2
    return 1
  fi
}

case_rejects_large_nonallowlisted_file() {
  repo=$(new_repo rejects-large-nonallowlisted-file) || return 1
  mkdir -p "$repo/assets"
  dd if=/dev/zero of="$repo/assets/generated.png" bs=1048577 count=1 2>/dev/null
  git -C "$repo" add .

  run_checker "$repo" ./scripts/check-repository-hygiene.sh
  if [ "$status" -eq 0 ]; then
    printf 'expected nonzero exit for non-allowlisted file above 1 MiB\n%s\n' "$output" >&2
    return 1
  fi
  case "$output" in
    *"assets/generated.png"*"larger than 1048576 bytes"*"超过 1048576 字节"*) ;;
    *)
      printf 'expected path and bilingual byte-limit reason\n%s\n' "$output" >&2
      return 1
      ;;
  esac
}

case_allows_exact_large_file_within_cap() {
  repo=$(new_repo allows-exact-large-file-within-cap) || return 1
  mkdir -p "$repo/assets" "$repo/.github"
  dd if=/dev/zero of="$repo/assets/product.bin" bs=1048577 count=1 2>/dev/null
  printf 'assets/product.bin\t1048600\n' >"$repo/.github/repository-hygiene-allowlist.txt"
  git -C "$repo" add .

  run_checker "$repo" ./scripts/check-repository-hygiene.sh
  if [ "$status" -ne 0 ]; then
    printf 'expected exact allowlisted file within cap to pass, got %s\n%s\n' "$status" "$output" >&2
    return 1
  fi
}

case_rejects_exact_large_file_above_cap() {
  repo=$(new_repo rejects-exact-large-file-above-cap) || return 1
  mkdir -p "$repo/assets" "$repo/.github"
  dd if=/dev/zero of="$repo/assets/product.bin" bs=1048577 count=1 2>/dev/null
  printf 'assets/product.bin\t1048576\n' >"$repo/.github/repository-hygiene-allowlist.txt"
  git -C "$repo" add .

  run_checker "$repo" ./scripts/check-repository-hygiene.sh
  if [ "$status" -eq 0 ]; then
    printf 'expected exact allowlisted file above cap to fail\n%s\n' "$output" >&2
    return 1
  fi
  case "$output" in
    *"assets/product.bin"*"allowlist maximum 1048576 bytes"*"白名单上限 1048576 字节"*) ;;
    *)
      printf 'expected path and bilingual allowlist-cap reason\n%s\n' "$output" >&2
      return 1
      ;;
  esac
}

case_rejects_text_over_line_limit() {
  repo=$(new_repo rejects-text-over-line-limit) || return 1
  mkdir -p "$repo/generated"
  awk 'BEGIN { for (i = 1; i <= 20001; i++) print "line" }' >"$repo/generated/transcript.txt"
  git -C "$repo" add .

  run_checker "$repo" ./scripts/check-repository-hygiene.sh
  if [ "$status" -eq 0 ]; then
    printf 'expected nonzero exit for text above 20000 lines\n%s\n' "$output" >&2
    return 1
  fi
  case "$output" in
    *"generated/transcript.txt"*"more than 20000 lines"*"超过 20000 行"*) ;;
    *)
      printf 'expected path and bilingual line-limit reason\n%s\n' "$output" >&2
      return 1
      ;;
  esac
}

case_allows_text_at_line_limit() {
  repo=$(new_repo allows-text-at-line-limit) || return 1
  mkdir -p "$repo/generated"
  awk 'BEGIN { for (i = 1; i <= 20000; i++) print "line" }' >"$repo/generated/normative.txt"
  git -C "$repo" add .

  run_checker "$repo" ./scripts/check-repository-hygiene.sh
  if [ "$status" -ne 0 ]; then
    printf 'expected exactly 20000 lines to pass, got %s\n%s\n' "$status" "$output" >&2
    return 1
  fi
}

case_does_not_line_count_allowlisted_binary() {
  repo=$(new_repo does-not-line-count-allowlisted-binary) || return 1
  mkdir -p "$repo/assets" "$repo/.github"
  dd if=/dev/zero of="$repo/assets/product.bin" bs=1048577 count=1 2>/dev/null
  awk 'BEGIN { for (i = 1; i <= 20001; i++) print "" }' >>"$repo/assets/product.bin"
  printf 'assets/product.bin\t1100000\n' >"$repo/.github/repository-hygiene-allowlist.txt"
  git -C "$repo" add .

  run_checker "$repo" ./scripts/check-repository-hygiene.sh
  if [ "$status" -ne 0 ]; then
    printf 'expected exact allowlisted binary to skip line gate, got %s\n%s\n' "$status" "$output" >&2
    return 1
  fi
}

case_rejects_allowlisted_large_text_over_line_limit() {
  repo=$(new_repo rejects-allowlisted-large-text) || return 1
  mkdir -p "$repo/assets" "$repo/.github"
  awk 'BEGIN { line = "1234567890123456789012345678901234567890"; for (i = 1; i <= 30001; i++) print line }' >"$repo/assets/product.txt"
  printf 'assets/product.txt\t2000000\n' >"$repo/.github/repository-hygiene-allowlist.txt"
  git -C "$repo" add .

  run_checker "$repo" ./scripts/check-repository-hygiene.sh
  if [ "$status" -eq 0 ]; then
    printf 'expected allowlisted text above 20000 lines to fail\n%s\n' "$output" >&2
    return 1
  fi
  case "$output" in
    *"assets/product.txt"*"more than 20000 lines"*"超过 20000 行"*) ;;
    *)
      printf 'expected allowlisted oversized text to fail the bilingual line gate\n%s\n' "$output" >&2
      return 1
      ;;
  esac
}

case_uses_indexed_allowlist_policy() {
  repo=$(new_repo uses-indexed-allowlist-policy) || return 1
  mkdir -p "$repo/assets" "$repo/.github"
  dd if=/dev/zero of="$repo/assets/product.bin" bs=1048577 count=1 2>/dev/null
  printf 'assets/product.bin\t1048576\n' >"$repo/.github/repository-hygiene-allowlist.txt"
  git -C "$repo" add .
  printf 'assets/product.bin\t2000000\n' >"$repo/.github/repository-hygiene-allowlist.txt"

  run_checker "$repo" ./scripts/check-repository-hygiene.sh
  if [ "$status" -eq 0 ]; then
    printf 'expected indexed allowlist cap to override looser working-tree content\n%s\n' "$output" >&2
    return 1
  fi
  case "$output" in
    *"assets/product.bin"*"allowlist maximum 1048576 bytes"*) ;;
    *)
      printf 'expected indexed allowlist cap despite looser working-tree content\n%s\n' "$output" >&2
      return 1
      ;;
  esac
}

case_runs_from_repository_subdirectory() {
  repo=$(new_repo runs-from-repository-subdirectory) || return 1
  mkdir -p "$repo/internal/example"
  printf 'package example\n' >"$repo/internal/example/example.go"
  git -C "$repo" add .

  run_checker "$repo/internal" ../scripts/check-repository-hygiene.sh
  if [ "$status" -ne 0 ]; then
    printf 'expected invocation from repository subdirectory to pass, got %s\n%s\n' "$status" "$output" >&2
    return 1
  fi
}

case_rejects_forbidden_path_with_spaces() {
  repo=$(new_repo rejects-forbidden-path-with-spaces) || return 1
  forbidden_path="docs/project-design/sources/raw evidence.txt"
  mkdir -p "$(dirname -- "$repo/$forbidden_path")"
  printf 'raw evidence\n' >"$repo/$forbidden_path"
  git -C "$repo" add .

  run_checker "$repo" ./scripts/check-repository-hygiene.sh
  if [ "$status" -eq 0 ]; then
    printf 'expected nonzero exit for forbidden path with spaces\n%s\n' "$output" >&2
    return 1
  fi
  case "$output" in
    *"docs/project-design/sources/raw\\ evidence.txt"*"forbidden evidence"*) ;;
    *)
      printf 'expected shell-escaped path with spaces and reason\n%s\n' "$output" >&2
      return 1
      ;;
  esac
}

case_rejects_forbidden_path_with_newline() {
  repo=$(new_repo rejects-forbidden-path-with-newline) || return 1
  forbidden_path=$(printf 'docs/project-design/sources/raw evidence\ncapture.json')
  mkdir -p "$(dirname -- "$repo/$forbidden_path")"
  printf 'raw evidence\n' >"$repo/$forbidden_path"
  git -C "$repo" add .

  run_checker "$repo" ./scripts/check-repository-hygiene.sh
  if [ "$status" -eq 0 ]; then
    printf 'expected nonzero exit for forbidden path with newline\n%s\n' "$output" >&2
    return 1
  fi
  case "$output" in
    *"raw evidence\\ncapture.json"*"forbidden evidence"*) ;;
    *)
      printf 'expected newline-safe escaped path and reason\n%s\n' "$output" >&2
      return 1
      ;;
  esac
}

run_case() {
  case_name=$1
  case_function=$2
  shift 2
  started_at=$(now_milliseconds)

  if "$case_function" "$@"; then
    finished_at=$(now_milliseconds)
    duration=$((finished_at - started_at))
    if [ "$duration" -ge 1000 ]; then
      printf 'not ok - %s (%sms; must be below 1000ms)\n' "$case_name" "$duration"
      failed=$((failed + 1))
    else
      printf 'ok - %s (%sms)\n' "$case_name" "$duration"
      passed=$((passed + 1))
    fi
  else
    finished_at=$(now_milliseconds)
    duration=$((finished_at - started_at))
    printf 'not ok - %s (%sms)\n' "$case_name" "$duration"
    failed=$((failed + 1))
  fi
}

run_case "allows ordinary source and normative ADR files" case_allows_product_and_normative_files
run_case "rejects project-design sources" case_rejects_forbidden_path \
  "docs/project-design/sources/interview.txt"
run_case "rejects evaluation run evidence" case_rejects_forbidden_path \
  "docs/project-design/implementation/evaluation/runs/2026-07-25/result.txt"
run_case "allows evaluation run templates" case_allows_path \
  "docs/project-design/implementation/evaluation/runs/templates/checklist.md"
run_case "rejects discovery JSON artifacts" case_rejects_forbidden_path \
  "docs/project-design/implementation/skills/discovery/findings.json"
run_case "rejects discovery JSONL artifacts" case_rejects_forbidden_path \
  "docs/project-design/implementation/skills/discovery/findings.jsonl"
run_case "allows discovery normative documents" case_allows_path \
  "docs/project-design/implementation/skills/discovery/README.md"
run_case "rejects deep-audit JSON artifacts" case_rejects_forbidden_path \
  "docs/project-design/implementation/skills/deep-audit/audit.json"
run_case "rejects deep-audit JSONL artifacts" case_rejects_forbidden_path \
  "docs/project-design/implementation/skills/deep-audit/audit.jsonl"
run_case "allows deep-audit normative documents" case_allows_path \
  "docs/project-design/implementation/skills/deep-audit/README.md"
run_case "rejects docs superpowers evidence" case_rejects_forbidden_path \
  "docs/superpowers/session/raw.txt"
run_case "rejects non-allowlisted files above 1 MiB" case_rejects_large_nonallowlisted_file
run_case "allows exact allowlisted large file within cap" case_allows_exact_large_file_within_cap
run_case "rejects exact allowlisted large file above cap" case_rejects_exact_large_file_above_cap
run_case "rejects text artifacts over 20000 lines" case_rejects_text_over_line_limit
run_case "allows text artifacts at exactly 20000 lines" case_allows_text_at_line_limit
run_case "does not line-count exact allowlisted binary assets" case_does_not_line_count_allowlisted_binary
run_case "rejects exact allowlisted large text over 20000 lines" case_rejects_allowlisted_large_text_over_line_limit
run_case "uses indexed allowlist policy instead of working-tree overrides" case_uses_indexed_allowlist_policy
run_case "allows ordinary tracked paths with spaces" case_allows_path \
  "docs/notes/normative guide.md"
run_case "rejects forbidden tracked paths with spaces" case_rejects_forbidden_path_with_spaces
run_case "rejects forbidden tracked paths with embedded newlines" case_rejects_forbidden_path_with_newline
run_case "runs correctly from a repository subdirectory" case_runs_from_repository_subdirectory

printf 'Summary: %s passed, %s failed, %s skipped\n' "$passed" "$failed" "$skipped"
test "$failed" -eq 0
