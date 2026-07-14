#!/usr/bin/env sh
set -eu

WORKSPACE_LABEL="code-review"
SLEEP_SECONDS="${HERDR_MR_REVIEW_SLEEP:-1}"

die() { echo "herdr-mr-review: $*" >&2; exit 1; }

command -v herdr  >/dev/null 2>&1 || die "'herdr' não encontrado no PATH"
command -v jq     >/dev/null 2>&1 || die "'jq' não encontrado no PATH"
command -v claude >/dev/null 2>&1 || die "'claude' não encontrado no PATH"

repo_path="${1:-}"
pr_number="${2:-}"

[ -n "$repo_path" ] || die "repo_path vazio — mapeie 'owner/repo -> clone local' em repoPaths no config.yml"
[ -n "$pr_number" ] || die "pr_number ausente"
[ -d "$repo_path" ] || die "diretório local não existe: $repo_path (repoPaths aponta para caminho inválido?)"

tab_label="MR-$pr_number"

ws=$(herdr workspace list | jq -r --arg l "$WORKSPACE_LABEL" \
    '.result.workspaces[]? | select(.label == $l) | .workspace_id' | head -n1)
if [ -z "$ws" ]; then
    ws=$(herdr workspace create --label "$WORKSPACE_LABEL" --cwd "$repo_path" --no-focus |
        jq -r '.result.workspace.workspace_id')
    [ -n "$ws" ] && [ "$ws" != "null" ] || die "falha ao criar o workspace $WORKSPACE_LABEL"
fi

tab=$(herdr tab list --workspace "$ws" | jq -r --arg l "$tab_label" \
    '.result.tabs[]? | select(.label == $l) | .tab_id' | head -n1)
if [ -n "$tab" ]; then
    herdr tab focus "$tab" >/dev/null
    exit 0
fi

pane=$(herdr tab create --workspace "$ws" --label "$tab_label" --cwd "$repo_path" --focus |
    jq -r '.result.root_pane.pane_id')
[ -n "$pane" ] && [ "$pane" != "null" ] || die "falha ao criar a tab $tab_label"

sleep "$SLEEP_SECONDS"
herdr pane run "$pane" "claude \"/mr-review $pr_number\" --dangerously-skip-permissions" >/dev/null
