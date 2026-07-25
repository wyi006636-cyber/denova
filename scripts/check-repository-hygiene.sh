#!/usr/bin/env bash

set -uo pipefail

if ! repository_root=$(git rev-parse --show-toplevel 2>/dev/null); then
  printf '%s\n' 'Repository hygiene check failed: run this command inside a Git repository. / 仓库卫生检查失败：请在 Git 仓库内运行此命令。' >&2
  exit 2
fi

violations=0
byte_limit=1048576
line_limit=20000
allowlist_file=.github/repository-hygiene-allowlist.txt

report_forbidden_evidence() {
  path=$1
  printf -v quoted_path '%q' "$path"
  printf 'Repository hygiene violation: %s — forbidden evidence path; keep raw evidence external. / 仓库卫生违规：%s — 禁止提交原始证据，请存放到 Git 仓库外。\n' \
    "$quoted_path" "$quoted_path" >&2
  violations=$((violations + 1))
}

allowlist_max_for_path() {
  candidate_path=$1
  allowlist_max=
  git -C "$repository_root" cat-file -e ":$allowlist_file" 2>/dev/null || return 1

  while IFS=$'\t' read -r allowed_path allowed_max extra; do
    case "$allowed_path" in
      '' | \#*) continue ;;
    esac
    [ -z "${extra:-}" ] || continue
    case "$allowed_max" in
      '' | *[!0-9]*) continue ;;
    esac
    if [ "$allowed_path" = "$candidate_path" ]; then
      allowlist_max=$allowed_max
      return 0
    fi
  done < <(git -C "$repository_root" cat-file blob ":$allowlist_file")

  return 1
}

report_large_file() {
  path=$1
  size=$2
  printf -v quoted_path '%q' "$path"
  printf 'Repository hygiene violation: %s — %s bytes, larger than 1048576 bytes; move generated evidence outside Git. / 仓库卫生违规：%s — %s 字节，超过 1048576 字节；请将生成证据移出 Git。\n' \
    "$quoted_path" "$size" "$quoted_path" "$size" >&2
  violations=$((violations + 1))
}

report_allowlist_cap() {
  path=$1
  size=$2
  maximum=$3
  printf -v quoted_path '%q' "$path"
  printf 'Repository hygiene violation: %s — %s bytes exceeds allowlist maximum %s bytes. / 仓库卫生违规：%s — %s 字节，超过白名单上限 %s 字节。\n' \
    "$quoted_path" "$size" "$maximum" "$quoted_path" "$size" "$maximum" >&2
  violations=$((violations + 1))
}

report_long_text() {
  path=$1
  lines=$2
  printf -v quoted_path '%q' "$path"
  printf 'Repository hygiene violation: %s — %s lines, more than 20000 lines; keep generated text outside Git. / 仓库卫生违规：%s — %s 行，超过 20000 行；请将生成文本移出 Git。\n' \
    "$quoted_path" "$lines" "$quoted_path" "$lines" >&2
  violations=$((violations + 1))
}

is_text_blob() {
  git -C "$repository_root" cat-file blob ":$1" | LC_ALL=C grep -I '' >/dev/null
}

while IFS= read -r -d '' path; do
  case "$path" in
    docs/project-design/sources/*)
      report_forbidden_evidence "$path"
      ;;
    docs/project-design/implementation/evaluation/runs/templates/*)
      ;;
    docs/project-design/implementation/evaluation/runs/*)
      report_forbidden_evidence "$path"
      ;;
    docs/project-design/implementation/skills/discovery/*.json | \
      docs/project-design/implementation/skills/discovery/*.jsonl | \
      docs/project-design/implementation/skills/deep-audit/*.json | \
      docs/project-design/implementation/skills/deep-audit/*.jsonl)
      report_forbidden_evidence "$path"
      ;;
    docs/superpowers/*)
      report_forbidden_evidence "$path"
      ;;
  esac

  blob_size=$(git -C "$repository_root" cat-file -s ":$path") || {
    printf -v quoted_path '%q' "$path"
    printf 'Repository hygiene check failed: cannot read indexed blob %s. / 仓库卫生检查失败：无法读取索引对象 %s。\n' \
      "$quoted_path" "$quoted_path" >&2
    exit 2
  }
  should_count_lines=false
  if [ "$blob_size" -gt "$byte_limit" ]; then
    if allowlist_max_for_path "$path"; then
      if [ "$blob_size" -gt "$allowlist_max" ]; then
        report_allowlist_cap "$path" "$blob_size" "$allowlist_max"
        continue
      fi
      if is_text_blob "$path"; then
        should_count_lines=true
      else
        continue
      fi
    else
      report_large_file "$path" "$blob_size"
      continue
    fi
  fi

  if [ "$should_count_lines" = false ] && [ "$blob_size" -ge "$line_limit" ] && is_text_blob "$path"; then
    should_count_lines=true
  fi
  if [ "$should_count_lines" = true ]; then
    line_count=$(git -C "$repository_root" cat-file blob ":$path" | awk 'END { print NR + 0 }') || {
      printf -v quoted_path '%q' "$path"
      printf 'Repository hygiene check failed: cannot count indexed lines for %s. / 仓库卫生检查失败：无法统计索引文件 %s 的行数。\n' \
        "$quoted_path" "$quoted_path" >&2
      exit 2
    }
    if [ "$line_count" -gt "$line_limit" ]; then
      report_long_text "$path" "$line_count"
    fi
  fi
done < <(git -C "$repository_root" ls-files -z)

if [ "$violations" -ne 0 ]; then
  printf 'Repository hygiene check failed: %s violation(s). / 仓库卫生检查失败：共 %s 项违规。\n' \
    "$violations" "$violations" >&2
  exit 1
fi

printf '%s\n' 'Repository hygiene check passed. / 仓库卫生检查通过。'
