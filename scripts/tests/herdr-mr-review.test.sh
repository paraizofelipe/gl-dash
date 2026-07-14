#!/usr/bin/env sh
set -u

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
REPO_ROOT=$(CDPATH= cd -- "$SCRIPT_DIR/../.." && pwd)
TARGET_SCRIPT="$REPO_ROOT/scripts/herdr-mr-review.sh"

REAL_JQ_PATH=$(command -v jq) || { echo "jq real nao encontrado no PATH do ambiente" >&2; exit 1; }
REAL_JQ_DIR=$(CDPATH= cd -- "$(dirname -- "$REAL_JQ_PATH")" && pwd)

WORKDIR=$(mktemp -d)
trap 'rm -rf "$WORKDIR"' EXIT INT TERM

PR_NUMBER=123
TOTAL=0
FAILURES=0

ok() {
    TOTAL=$((TOTAL + 1))
    printf 'ok - %s\n' "$1"
}

fail() {
    TOTAL=$((TOTAL + 1))
    FAILURES=$((FAILURES + 1))
    printf 'FAIL - %s\n' "$1"
    printf '       esperado: %s\n' "$2"
    printf '       obtido:   %s\n' "$3"
}

assert_eq() {
    desc="$1"
    expected="$2"
    actual="$3"
    if [ "$expected" = "$actual" ]; then
        ok "$desc"
    else
        fail "$desc" "$expected" "$actual"
    fi
}

assert_ne() {
    desc="$1"
    not_expected="$2"
    actual="$3"
    if [ "$not_expected" != "$actual" ]; then
        ok "$desc"
    else
        fail "$desc" "diferente de '$not_expected'" "$actual"
    fi
}

assert_contains() {
    desc="$1"
    haystack="$2"
    needle="$3"
    case "$haystack" in
        *"$needle"*)
            ok "$desc"
            ;;
        *)
            fail "$desc" "contendo '$needle'" "$haystack"
            ;;
    esac
}

line_count_or_zero() {
    if [ -f "$1" ]; then
        wc -l < "$1" | tr -d ' '
    else
        printf '0'
    fi
}

first_line_or_empty() {
    if [ -f "$1" ]; then
        head -n 1 "$1"
    else
        printf ''
    fi
}

write_fake_claude() {
    dir="$1"
    cat > "$dir/claude" <<'CLAUDE_STUB'
#!/usr/bin/env sh
exit 0
CLAUDE_STUB
    chmod +x "$dir/claude"
}

write_fake_herdr() {
    dir="$1"
    cat > "$dir/herdr" <<'HERDR_STUB'
#!/usr/bin/env sh
set -eu

state_dir="${HERDR_FAKE_STATE_DIR:?HERDR_FAKE_STATE_DIR not set}"
log_dir="$state_dir/log"
mkdir -p "$log_dir"

workspaces_file="$state_dir/workspaces.json"
[ -f "$workspaces_file" ] || printf '[]' > "$workspaces_file"

next_id() {
    seq_file="$state_dir/seq"
    if [ -f "$seq_file" ]; then
        n=$(cat "$seq_file")
    else
        n=0
    fi
    n=$((n + 1))
    printf '%s' "$n" > "$seq_file"
    printf '%s' "$n"
}

group="$1"
action="$2"
shift 2

case "$group.$action" in
    workspace.list)
        printf '1\n' >> "$log_dir/workspace_list"
        jq -n --slurpfile ws "$workspaces_file" '{result:{workspaces:$ws[0]}}'
        ;;
    workspace.create)
        label=""
        while [ $# -gt 0 ]; do
            case "$1" in
                --label)
                    label="$2"
                    shift 2
                    ;;
                --cwd)
                    shift 2
                    ;;
                --focus|--no-focus)
                    shift 1
                    ;;
                *)
                    shift 1
                    ;;
            esac
        done
        new_id="ws-$(next_id)"
        tmp_file="$workspaces_file.tmp"
        jq --arg id "$new_id" --arg l "$label" '. + [{label:$l, workspace_id:$id}]' "$workspaces_file" > "$tmp_file"
        mv "$tmp_file" "$workspaces_file"
        printf '%s\n' "$new_id" >> "$log_dir/workspace_create"
        jq -n --arg id "$new_id" '{result:{workspace:{workspace_id:$id}}}'
        ;;
    tab.list)
        ws=""
        while [ $# -gt 0 ]; do
            case "$1" in
                --workspace)
                    ws="$2"
                    shift 2
                    ;;
                *)
                    shift 1
                    ;;
            esac
        done
        tabs_file="$state_dir/tabs-$ws.json"
        [ -f "$tabs_file" ] || printf '[]' > "$tabs_file"
        printf '%s\n' "$ws" >> "$log_dir/tab_list"
        jq -n --slurpfile t "$tabs_file" '{result:{tabs:$t[0]}}'
        ;;
    tab.create)
        ws=""
        label=""
        while [ $# -gt 0 ]; do
            case "$1" in
                --workspace)
                    ws="$2"
                    shift 2
                    ;;
                --label)
                    label="$2"
                    shift 2
                    ;;
                --cwd)
                    shift 2
                    ;;
                --focus|--no-focus)
                    shift 1
                    ;;
                *)
                    shift 1
                    ;;
            esac
        done
        tabs_file="$state_dir/tabs-$ws.json"
        [ -f "$tabs_file" ] || printf '[]' > "$tabs_file"
        new_tab="tab-$(next_id)"
        new_pane="pane-$(next_id)"
        tmp_file="$tabs_file.tmp"
        jq --arg id "$new_tab" --arg l "$label" '. + [{label:$l, tab_id:$id}]' "$tabs_file" > "$tmp_file"
        mv "$tmp_file" "$tabs_file"
        printf '%s\n' "$new_tab" >> "$log_dir/tab_create"
        jq -n --arg pid "$new_pane" '{result:{root_pane:{pane_id:$pid}}}'
        ;;
    tab.focus)
        tab_id="$1"
        printf '%s\n' "$tab_id" >> "$log_dir/tab_focus"
        jq -n '{result:{}}'
        ;;
    pane.run)
        pane_id="$1"
        shift 1
        command_arg="$1"
        printf '%s\n' "$pane_id" >> "$log_dir/pane_run_pane_id"
        printf '%s\n' "$command_arg" >> "$log_dir/pane_run"
        jq -n '{result:{}}'
        ;;
    *)
        printf 'fake herdr: comando nao suportado: %s %s\n' "$group" "$action" >&2
        exit 1
        ;;
esac
HERDR_STUB
    chmod +x "$dir/herdr"
}

scenario_first_invocation_creates_workspace_and_tab_and_runs_claude() {
    scenario_dir="$WORKDIR/scenario1"
    mkdir -p "$scenario_dir/stub" "$scenario_dir/state" "$scenario_dir/repo"
    write_fake_herdr "$scenario_dir/stub"
    write_fake_claude "$scenario_dir/stub"

    stdout_file="$scenario_dir/stdout"
    stderr_file="$scenario_dir/stderr"

    PATH="$scenario_dir/stub:$REAL_JQ_DIR" \
        HERDR_FAKE_STATE_DIR="$scenario_dir/state" \
        HERDR_MR_REVIEW_SLEEP=0 \
        "$TARGET_SCRIPT" "$scenario_dir/repo" "$PR_NUMBER" >"$stdout_file" 2>"$stderr_file"
    exit_code=$?

    assert_eq "cenario1: script sai com codigo 0 na primeira invocacao" "0" "$exit_code"

    workspace_create_count=$(line_count_or_zero "$scenario_dir/state/log/workspace_create")
    assert_eq "cenario1: workspace create e chamado exatamente uma vez quando o workspace nao existe" "1" "$workspace_create_count"

    tab_create_count=$(line_count_or_zero "$scenario_dir/state/log/tab_create")
    assert_eq "cenario1: tab create e chamado exatamente uma vez quando a tab nao existe" "1" "$tab_create_count"

    pane_run_file="$scenario_dir/state/log/pane_run"
    pane_run_count=$(line_count_or_zero "$pane_run_file")
    assert_eq "cenario1: pane run e chamado exatamente uma vez" "1" "$pane_run_count"

    pane_run_content=$(first_line_or_empty "$pane_run_file")
    expected_command="claude \"/mr-review $PR_NUMBER\" --dangerously-skip-permissions"
    assert_eq "cenario1: comando enviado ao pane run e exatamente o esperado" "$expected_command" "$pane_run_content"
}

scenario_existing_tab_focuses_without_running_pane_again() {
    scenario_dir="$WORKDIR/scenario2"
    mkdir -p "$scenario_dir/stub" "$scenario_dir/state" "$scenario_dir/repo"
    write_fake_herdr "$scenario_dir/stub"
    write_fake_claude "$scenario_dir/stub"

    ws_id="ws-seed"
    tab_id="tab-seed"
    printf '[{"label":"code-review","workspace_id":"%s"}]' "$ws_id" > "$scenario_dir/state/workspaces.json"
    printf '[{"label":"MR-%s","tab_id":"%s"}]' "$PR_NUMBER" "$tab_id" > "$scenario_dir/state/tabs-$ws_id.json"

    stdout_file="$scenario_dir/stdout"
    stderr_file="$scenario_dir/stderr"

    PATH="$scenario_dir/stub:$REAL_JQ_DIR" \
        HERDR_FAKE_STATE_DIR="$scenario_dir/state" \
        HERDR_MR_REVIEW_SLEEP=0 \
        "$TARGET_SCRIPT" "$scenario_dir/repo" "$PR_NUMBER" >"$stdout_file" 2>"$stderr_file"
    exit_code=$?

    assert_eq "cenario2: script sai com codigo 0 quando a tab ja existe" "0" "$exit_code"

    tab_focus_file="$scenario_dir/state/log/tab_focus"
    tab_focus_count=$(line_count_or_zero "$tab_focus_file")
    assert_eq "cenario2: tab focus e chamado exatamente uma vez" "1" "$tab_focus_count"

    focused_tab=$(first_line_or_empty "$tab_focus_file")
    assert_eq "cenario2: tab focus e chamado com o tab_id existente" "$tab_id" "$focused_tab"

    workspace_create_count=$(line_count_or_zero "$scenario_dir/state/log/workspace_create")
    assert_eq "cenario2: workspace create nao e chamado quando workspace e tab ja existem" "0" "$workspace_create_count"

    tab_create_count=$(line_count_or_zero "$scenario_dir/state/log/tab_create")
    assert_eq "cenario2: tab create nao e chamado quando a tab ja existe" "0" "$tab_create_count"

    pane_run_count=$(line_count_or_zero "$scenario_dir/state/log/pane_run")
    assert_eq "cenario2: pane run nao e chamado quando a tab ja existe" "0" "$pane_run_count"
}

scenario_existing_workspace_is_reused_without_recreating() {
    scenario_dir="$WORKDIR/scenario3"
    mkdir -p "$scenario_dir/stub" "$scenario_dir/state" "$scenario_dir/repo"
    write_fake_herdr "$scenario_dir/stub"
    write_fake_claude "$scenario_dir/stub"

    ws_id="ws-seed-reuse"
    printf '[{"label":"code-review","workspace_id":"%s"}]' "$ws_id" > "$scenario_dir/state/workspaces.json"

    stdout_file="$scenario_dir/stdout"
    stderr_file="$scenario_dir/stderr"

    PATH="$scenario_dir/stub:$REAL_JQ_DIR" \
        HERDR_FAKE_STATE_DIR="$scenario_dir/state" \
        HERDR_MR_REVIEW_SLEEP=0 \
        "$TARGET_SCRIPT" "$scenario_dir/repo" "$PR_NUMBER" >"$stdout_file" 2>"$stderr_file"
    exit_code=$?

    assert_eq "cenario3: script sai com codigo 0 reutilizando workspace existente" "0" "$exit_code"

    workspace_create_count=$(line_count_or_zero "$scenario_dir/state/log/workspace_create")
    assert_eq "cenario3: workspace create nao e chamado quando o workspace ja existe" "0" "$workspace_create_count"

    tab_create_count=$(line_count_or_zero "$scenario_dir/state/log/tab_create")
    assert_eq "cenario3: tab create e chamado exatamente uma vez para a tab nova" "1" "$tab_create_count"

    pane_run_count=$(line_count_or_zero "$scenario_dir/state/log/pane_run")
    assert_eq "cenario3: pane run e chamado para a nova tab no workspace reutilizado" "1" "$pane_run_count"
}

scenario_invalid_repo_path_fails_with_clear_message() {
    scenario_dir="$WORKDIR/scenario4"
    mkdir -p "$scenario_dir/stub" "$scenario_dir/state"
    write_fake_herdr "$scenario_dir/stub"
    write_fake_claude "$scenario_dir/stub"

    missing_repo="$scenario_dir/does-not-exist"

    stdout_file="$scenario_dir/stdout"
    stderr_file="$scenario_dir/stderr"

    PATH="$scenario_dir/stub:$REAL_JQ_DIR" \
        HERDR_FAKE_STATE_DIR="$scenario_dir/state" \
        HERDR_MR_REVIEW_SLEEP=0 \
        "$TARGET_SCRIPT" "$missing_repo" "$PR_NUMBER" >"$stdout_file" 2>"$stderr_file"
    exit_code=$?

    assert_ne "cenario4: script sai com codigo diferente de 0 para repo_path inexistente" "0" "$exit_code"

    stderr_content=$(cat "$stderr_file")
    assert_contains "cenario4: stderr menciona diretorio local inexistente" "$stderr_content" "diretório local não existe"
}

scenario_missing_herdr_dependency_fails_with_clear_message() {
    scenario_dir="$WORKDIR/scenario5"
    mkdir -p "$scenario_dir/stub" "$scenario_dir/state" "$scenario_dir/repo"
    write_fake_claude "$scenario_dir/stub"

    stdout_file="$scenario_dir/stdout"
    stderr_file="$scenario_dir/stderr"

    PATH="$scenario_dir/stub:$REAL_JQ_DIR" \
        HERDR_FAKE_STATE_DIR="$scenario_dir/state" \
        HERDR_MR_REVIEW_SLEEP=0 \
        "$TARGET_SCRIPT" "$scenario_dir/repo" "$PR_NUMBER" >"$stdout_file" 2>"$stderr_file"
    exit_code=$?

    assert_ne "cenario5: script sai com codigo diferente de 0 sem o herdr no PATH" "0" "$exit_code"

    stderr_content=$(cat "$stderr_file")
    assert_contains "cenario5: stderr menciona dependencia herdr ausente" "$stderr_content" "'herdr' não encontrado"
}

scenario_missing_pr_number_argument_fails_with_clear_message() {
    scenario_dir="$WORKDIR/scenario6"
    mkdir -p "$scenario_dir/stub" "$scenario_dir/state" "$scenario_dir/repo"
    write_fake_herdr "$scenario_dir/stub"
    write_fake_claude "$scenario_dir/stub"

    stdout_file="$scenario_dir/stdout"
    stderr_file="$scenario_dir/stderr"

    PATH="$scenario_dir/stub:$REAL_JQ_DIR" \
        HERDR_FAKE_STATE_DIR="$scenario_dir/state" \
        HERDR_MR_REVIEW_SLEEP=0 \
        "$TARGET_SCRIPT" "$scenario_dir/repo" >"$stdout_file" 2>"$stderr_file"
    exit_code=$?

    assert_ne "cenario6: script sai com codigo diferente de 0 sem pr_number" "0" "$exit_code"

    stderr_content=$(cat "$stderr_file")
    assert_contains "cenario6: stderr menciona pr_number ausente" "$stderr_content" "pr_number ausente"
}

scenario_first_invocation_creates_workspace_and_tab_and_runs_claude
scenario_existing_tab_focuses_without_running_pane_again
scenario_existing_workspace_is_reused_without_recreating
scenario_invalid_repo_path_fails_with_clear_message
scenario_missing_herdr_dependency_fails_with_clear_message
scenario_missing_pr_number_argument_fails_with_clear_message

printf '\n%s de %s verificacoes passaram\n' "$((TOTAL - FAILURES))" "$TOTAL"

if [ "$FAILURES" -eq 0 ]; then
    printf 'RESULTADO: todos os cenarios passaram\n'
    exit 0
else
    printf 'RESULTADO: %s verificacao(oes) falharam\n' "$FAILURES"
    exit 1
fi
