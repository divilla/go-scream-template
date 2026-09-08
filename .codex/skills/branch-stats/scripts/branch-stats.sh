#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'USAGE'
Usage: branch-stats.sh [--branch REF] [--base REF]

Calculate a branch's aggregate changed-line statistics from its merge base.
Defaults: --branch HEAD; --base repository remote default branch.
USAGE
}

branch_ref=HEAD
base_ref=

while (($#)); do
  case "$1" in
    --branch)
      (($# >= 2)) || { echo "branch-stats: --branch requires a ref" >&2; exit 2; }
      branch_ref=$2
      shift 2
      ;;
    --base)
      (($# >= 2)) || { echo "branch-stats: --base requires a ref" >&2; exit 2; }
      base_ref=$2
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "branch-stats: unknown argument: $1" >&2
      usage >&2
      exit 2
      ;;
  esac
done

for dependency in git cloc jq awk; do
  command -v "$dependency" >/dev/null 2>&1 || {
    echo "branch-stats: missing required command: $dependency" >&2
    exit 127
  }
done

git rev-parse --is-inside-work-tree >/dev/null 2>&1 || {
  echo "branch-stats: run inside a Git worktree" >&2
  exit 2
}

repo_root=$(git rev-parse --show-toplevel)
cd "$repo_root"

if [[ -z "$base_ref" ]]; then
  base_ref=$(git symbolic-ref --quiet --short refs/remotes/origin/HEAD 2>/dev/null || true)
  if [[ -z "$base_ref" ]]; then
    for candidate in origin/master origin/main master main; do
      if git rev-parse --verify --quiet --end-of-options "${candidate}^{commit}" >/dev/null; then
        base_ref=$candidate
        break
      fi
    done
  fi
fi

[[ -n "$base_ref" ]] || {
  echo "branch-stats: cannot determine a base ref; pass --base REF" >&2
  exit 2
}

branch_commit=$(git rev-parse --verify --end-of-options "${branch_ref}^{commit}") || {
  echo "branch-stats: invalid branch ref: $branch_ref" >&2
  exit 2
}
base_commit=$(git rev-parse --verify --end-of-options "${base_ref}^{commit}") || {
  echo "branch-stats: invalid base ref: $base_ref" >&2
  exit 2
}
merge_base=$(git merge-base "$base_commit" "$branch_commit") || {
  echo "branch-stats: $branch_ref and $base_ref have no merge base" >&2
  exit 2
}

commit_count=$(git rev-list --count "${merge_base}..${branch_commit}")
short_branch=$(git rev-parse --short "$branch_commit")
short_merge_base=$(git rev-parse --short "$merge_base")
if [[ "$branch_ref" == HEAD ]]; then
  branch_label=$(git branch --show-current)
  [[ -n "$branch_label" ]] || branch_label=HEAD
else
  branch_label=$branch_ref
fi

if [[ -n "$(git status --porcelain)" ]]; then
  worktree_state=dirty
else
  worktree_state=clean
fi

tmp_dir=$(mktemp -d "${TMPDIR:-/tmp}/branch-stats.XXXXXX")
trap 'rm -rf -- "$tmp_dir"' EXIT
numstat_file="$tmp_dir/numstat.tsv"
cloc_file="$tmp_dir/cloc.json"
comments_file="$tmp_dir/comments.tsv"

# NUL records expose both rename paths without Git's compact display syntax.
# cloc keys renamed-file counts by the old path; classify by the destination.
git diff --numstat -z --find-renames "$merge_base" "$branch_commit" |
  while IFS= read -r -d '' record; do
    added=${record%%$'\t'*}
    remainder=${record#*$'\t'}
    deleted=${remainder%%$'\t'*}
    path=${remainder#*$'\t'}
    old_path=$path
    if [[ -z "$path" ]]; then
      IFS= read -r -d '' old_path
      IFS= read -r -d '' path
    fi
    printf '%s\0%s\0%s\0%s\0' "$added" "$deleted" "$path" "$old_path"
  done |
  jq -Rrs 'split("\u0000") | .[:-1] | range(0; length; 4) as $i | .[$i:$i+4] | @tsv' >"$numstat_file"

if [[ -s "$numstat_file" ]]; then
  cloc --config /dev/null --git --diff "$merge_base" "$branch_commit" \
    --diff-timeout=0 --by-file --json --quiet --report-file="$cloc_file"

  jq -r '
    . as $r
    | [
        (($r.added // {}) | keys[]),
        (($r.modified // {}) | keys[]),
        (($r.removed // {}) | keys[])
      ]
    | unique[] as $path
    | [
        $path,
        (($r.added[$path].comment // 0) + ($r.modified[$path].comment // 0)),
        (($r.removed[$path].comment // 0) + ($r.modified[$path].comment // 0))
      ]
    | @tsv
  ' "$cloc_file" >"$comments_file"
else
  : >"$comments_file"
fi

printf 'Branch `%s` (`%s`), %s commits against merge base `%s` with `%s`.\n\n' \
  "$branch_label" "$short_branch" "$commit_count" "$short_merge_base" "$base_ref"
printf 'Working tree: **%s**. Working-tree changes are excluded.\n\n' "$worktree_state"

awk -F '\t' '
  BEGIN {
    labels[1] = "Production application code"
    labels[2] = "Unit tests"
    labels[3] = "Integration tests and fixtures"
    labels[4] = "Developer tooling"
    labels[5] = "Skeleton - production-shaped"
    labels[6] = "Skeleton - unit-test-shaped"
    labels[7] = "Documentation, specifications, and logs"
    labels[8] = "Dependency metadata"
  }

  FILENAME == ARGV[1] {
    comment_added[$1] = $2 + 0
    comment_deleted[$1] = $3 + 0
    next
  }

  function category(path) {
    if (path ~ /^skeleton\//) {
      return path ~ /_test\.go$/ ? 6 : 5
    }
    if (path ~ /^(integration-test|int-tests)\//) return 3
    if (path ~ /(_test|\.test|\.spec)\.[^/]+$/ || path ~ /^scripts\/test\./) return 2
    if (path ~ /^(cmd|config|internal|pkg)\// && path ~ /\.go$/) return 1
    if (path == "Makefile" || path ~ /^(scripts\/|\.codex\/skills\/).*\.(sh|pl|pm|py|awk)$/) return 4
    if (path == "go.mod" || path == "go.sum") return 8
    return 7
  }

  {
    path = $3
    bucket = category(path)
    files[bucket]++

    if ($1 == "-" || $2 == "-") {
      binary_files++
      next
    }

    added = $1 + 0
    deleted = $2 + 0
    comment_path = NF >= 4 ? $4 : path
    comments_added = comment_added[comment_path] + 0
    comments_deleted = comment_deleted[comment_path] + 0

    if (comments_added > added || comments_deleted > deleted) {
      printf "branch-stats: comment count exceeds Git line count for %s\n", path > "/dev/stderr"
      invalid = 1
      next
    }

    add_total[bucket] += added
    del_total[bucket] += deleted
    add_comments[bucket] += comments_added
    del_comments[bucket] += comments_deleted
  }

  END {
    if (invalid) exit 3

    print "| Category | Files | Added non-comment | Added comments | Deleted non-comment | Deleted comments | Comments changed | Total changed | Net |"
    print "|---|---:|---:|---:|---:|---:|---:|---:|---:|"

    for (i = 1; i <= 8; i++) {
      add_non_comment = add_total[i] - add_comments[i]
      del_non_comment = del_total[i] - del_comments[i]
      changed_comments = add_comments[i] + del_comments[i]
      changed_total = add_total[i] + del_total[i]
      net = add_total[i] - del_total[i]

      printf "| %s | %d | %d | %d | %d | %d | %d | %d | %+d |\n", \
        labels[i], files[i], add_non_comment, add_comments[i], \
        del_non_comment, del_comments[i], changed_comments, changed_total, net

      all_files += files[i]
      all_added += add_total[i]
      all_deleted += del_total[i]
      all_add_comments += add_comments[i]
      all_del_comments += del_comments[i]
    }

    all_add_non_comment = all_added - all_add_comments
    all_del_non_comment = all_deleted - all_del_comments
    all_comments = all_add_comments + all_del_comments
    all_changed = all_added + all_deleted
    all_net = all_added - all_deleted

    printf "| **Entire branch** | **%d** | **%d** | **%d** | **%d** | **%d** | **%d** | **%d** | **%+d** |\n", \
      all_files, all_add_non_comment, all_add_comments, all_del_non_comment, \
      all_del_comments, all_comments, all_changed, all_net

    if (all_changed > 0) {
      printf "\nComments account for **%d changed lines**, or **%.1f%%** of the branch diff.\n", \
        all_comments, 100 * all_comments / all_changed
    }
    if (binary_files > 0) {
      printf "\n%d binary file(s) are included in the file count but excluded from line totals.\n", binary_files
    }
  }
' "$comments_file" "$numstat_file"
