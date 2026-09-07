#!/usr/bin/env bash

set -euo pipefail

usage() {
    cat <<'EOF'
Usage:
  scripts/change-review.md [base-branch] [max-passes] [output-directory]

Example:
  scripts/change-review.md
  scripts/change-review.md main
  scripts/change-review.md origin/main 6 review-results

The output directory must not already exist.
EOF
}

if [[ $# -gt 3 ]]; then
    usage >&2
    exit 2
fi

base_branch=${1:-master}
max_passes=${2:-6}
review_pid=""
stream_pid=""
progress_pid=""
progress_done_file=""

stop_review() {
    trap - INT TERM HUP

    if [[ -n "$review_pid" ]] && kill -0 "$review_pid" 2>/dev/null; then
        kill -TERM "$review_pid" 2>/dev/null || true
        wait "$review_pid" 2>/dev/null || true
    fi

    if [[ -n "$progress_done_file" ]]; then
        : > "$progress_done_file"
    fi

    if [[ -n "$progress_pid" ]]; then
        wait "$progress_pid" 2>/dev/null || true
    fi

    if [[ -n "$stream_pid" ]] && kill -0 "$stream_pid" 2>/dev/null; then
        kill -TERM "$stream_pid" 2>/dev/null || true
        wait "$stream_pid" 2>/dev/null || true
    fi

    printf '\nStopping review loop...\n' >&2
    exit 130
}

trap stop_review INT TERM HUP

record_output_events() {
    local events_file=$1
    local line=""
    local in_dump=false
    local read_status=0

    while true; do
        if IFS= read -r -t 0.2 line; then
            if [[ "$in_dump" == false ]]; then
                printf '.' >> "$events_file"
                in_dump=true
            fi

            line=""
            continue
        else
            read_status=$?
        fi

        if [[ -n "$line" && "$in_dump" == false ]]; then
            printf '.' >> "$events_file"
        fi

        if ((read_status == 1)); then
            break
        fi

        in_dump=false
        line=""
    done
}

render_progress() {
    local events_file=$1
    local done_file=$2
    local -a frames=('·' '∙' '•' '●')
    local started=$SECONDS
    local frame_index=0
    local elapsed=0
    local minutes=0
    local seconds=0
    local dots=""

    printf '\n'

    while [[ ! -e "$done_file" ]]; do
        elapsed=$((SECONDS - started))
        minutes=$((elapsed / 60))
        seconds=$((elapsed % 60))
        dots=$(<"$events_file")

        printf '\r%02d:%02d %s%s' \
            "$minutes" "$seconds" "$dots" "${frames[$frame_index]}"

        frame_index=$(((frame_index + 1) % ${#frames[@]}))
        sleep 0.25
    done

    elapsed=$((SECONDS - started))
    minutes=$((elapsed / 60))
    seconds=$((elapsed % 60))
    dots=$(<"$events_file")

    printf '\r%02d:%02d %s\033[K\n' "$minutes" "$seconds" "$dots"
}

if ! [[ "$max_passes" =~ ^[1-9][0-9]*$ ]]; then
    printf 'Error: max-passes must be a positive integer.\n' >&2
    exit 2
fi

if ! command -v git >/dev/null 2>&1; then
    printf 'Error: git is not installed or is not in PATH.\n' >&2
    exit 1
fi

if ! command -v codex >/dev/null 2>&1; then
    printf 'Error: codex is not installed or is not in PATH.\n' >&2
    exit 1
fi

if ! git rev-parse --is-inside-work-tree >/dev/null 2>&1; then
    printf 'Error: run this script inside a Git repository.\n' >&2
    exit 1
fi

repo_root=$(git rev-parse --show-toplevel)
default_output_dir="$repo_root/agent/codex-review-results"
output_dir=${3:-$default_output_dir}

if [[ "$output_dir" == "$default_output_dir" ]]; then
    rm -rf -- "$default_output_dir"
fi

if ! git rev-parse --verify "${base_branch}^{commit}" >/dev/null 2>&1; then
    printf 'Error: base branch %q does not resolve to a commit.\n' \
        "$base_branch" >&2
    exit 1
fi

if ! merge_base=$(git merge-base HEAD "$base_branch") || \
    [[ -z "$merge_base" ]]; then
    printf 'Error: could not determine merge base with %q.\n' \
        "$base_branch" >&2
    exit 1
fi

if [[ -e "$output_dir" ]]; then
    printf 'Error: output directory already exists: %s\n' \
        "$output_dir" >&2
    exit 1
fi

mkdir -p "$output_dir"

ledger="$output_dir/merged-findings.md"
metadata="$output_dir/run-info.txt"

touch "$ledger"

{
    printf 'Base branch: %s\n' "$base_branch"
    printf 'Merge base: %s\n' "$merge_base"
    printf 'HEAD: %s\n' "$(git rev-parse HEAD)"
    printf 'Maximum passes: %s\n' "$max_passes"
} > "$metadata"

printf 'Base branch: %s\n' "$base_branch"
printf 'Merge base: %s\n' "$merge_base"
printf 'Output: %s\n\n' "$output_dir"

completed_passes=0
stopped_cleanly=false

for ((pass = 1; pass <= max_passes; pass++)); do
    prompt_file="$output_dir/pass-${pass}-prompt.md"
    result_file="$output_dir/pass-${pass}-result.md"
    stream_file="$output_dir/pass-${pass}-stream.log"
    progress_events_file="$output_dir/pass-${pass}-progress.events"
    progress_done_file="$output_dir/pass-${pass}-progress.done"
    output_pipe="$output_dir/pass-${pass}-output.pipe"

    : > "$stream_file"
    : > "$progress_events_file"
    mkfifo "$output_pipe"

    {
        printf '# Review target\n\n'
        printf 'Review the code changes against base branch `%s`.\n' \
            "$base_branch"
        printf 'The merge-base commit is `%s`.\n' "$merge_base"
        printf 'Run `git diff %s` to inspect the changes.\n\n' "$merge_base"

        printf '# Iterative-review instructions\n\n'
        printf 'This is review pass %d of at most %d.\n\n' \
            "$pass" "$max_passes"

        cat <<'EOF'
Report only newly discovered, actionable findings.

The previous-findings section below is an exclusion ledger:

- Do not repeat the same underlying defect.
- Treat substantially equivalent findings as duplicates even when their title,
  wording, priority, or line range differs.
- Do not suppress a genuinely distinct defect merely because it occurs in the
  same file or on the same line.
- Verify that every new finding still applies to the current code.
- Do not report findings merely to produce output.
- Do not modify the repository.
- Do not execute `scripts/change-review.md`.
- Do not invoke `codex` or start another Codex session from this review.

If no new findings remain:

- Return an empty findings list.
- Set `overall_explanation` to exactly `NO_NEW_FINDINGS`.

EOF

        printf '# Previous findings\n\n'

        if [[ -s "$ledger" ]]; then
            cat "$ledger"
        else
            printf '(No previous findings.)\n'
        fi
    } > "$prompt_file"

    printf 'Running review pass %d/%d...\n' "$pass" "$max_passes"

    (
        set -o pipefail
        tee "$stream_file" < "$output_pipe" | \
            record_output_events "$progress_events_file"
    ) &
    stream_pid=$!

    codex exec review \
        --ephemeral \
        --output-last-message "$result_file" \
        - < "$prompt_file" > "$output_pipe" 2>&1 &
    review_pid=$!

    render_progress "$progress_events_file" "$progress_done_file" &
    progress_pid=$!

    if wait "$review_pid"; then
        review_status=0
    else
        review_status=$?
    fi
    review_pid=""

    if wait "$stream_pid"; then
        stream_status=0
    else
        stream_status=$?
    fi
    stream_pid=""

    : > "$progress_done_file"
    wait "$progress_pid"
    progress_pid=""

    rm -f -- "$output_pipe" "$progress_events_file" "$progress_done_file"
    progress_done_file=""

    if ((review_status != 0)); then
        printf '\nError: review pass %d failed.\n' "$pass" >&2
        printf 'Prompt: %s\n' "$prompt_file" >&2
        printf 'Command output: %s\n' "$stream_file" >&2
        exit "$review_status"
    fi

    if ((stream_status != 0)); then
        printf '\nError: could not capture output for review pass %d.\n' \
            "$pass" >&2
        exit "$stream_status"
    fi

    completed_passes=$pass

    if [[ ! -s "$result_file" ]]; then
        printf '\nError: pass %d produced no captured output.\n' \
            "$pass" >&2
        exit 1
    fi

    result_text=$(<"$result_file")

    if [[ "$result_text" == 'NO_NEW_FINDINGS' ]]; then
        printf '\nNo new findings on pass %d. Review complete.\n' "$pass"
        stopped_cleanly=true
        break
    fi

    {
        printf '## Pass %d\n\n' "$pass"
        cat "$result_file"
        printf '\n\n'
    } >> "$ledger"

    printf 'Pass %d findings added to the ledger.\n\n' "$pass"
done

{
    printf 'Completed passes: %d\n' "$completed_passes"
    printf 'Stopped with no new findings: %s\n' "$stopped_cleanly"
} >> "$metadata"

printf '\nReview loop finished.\n'
printf 'Merged findings: %s\n' "$ledger"
printf 'Run information: %s\n' "$metadata"

if [[ "$stopped_cleanly" != true ]]; then
    printf 'Warning: maximum pass count reached before a clean pass.\n'
fi
