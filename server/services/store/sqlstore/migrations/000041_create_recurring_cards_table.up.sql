{{- /* Index of recurring cards for the scheduler.

      This table is a derived index, not a source of truth. The authoritative
      configuration lives in the card block's fields.recurrence; a row here only
      exists so that a per-minute scheduler can ask which cards are due without
      scanning every block on every board. QueryBlocksOptions can filter blocks
      by board, parent and type only, and there is no way to query inside the
      JSON fields column portably.

      Rebuilding this table from the blocks table is always safe. */ -}}

CREATE TABLE IF NOT EXISTS {{.prefix}}recurring_cards (
    card_id VARCHAR(36) NOT NULL,
    board_id VARCHAR(36) NOT NULL,

    {{- /* active is the conjunction of cardType == 'recurring' and
           recurrence.enabled, not a mirror of recurrence.enabled alone. It is
           what the scheduler keys off. */}}
    active BOOLEAN NOT NULL DEFAULT false,

    {{- /* Denormalised from config so the done-column trigger can short-circuit
           without parsing it. Never queried on its own, so it is not indexed. */}}
    mode VARCHAR(16) NOT NULL,

    config {{if .postgres}}JSON{{else}}TEXT{{end}} NOT NULL,

    {{- /* NULL means "not scheduled": an afterDone card that has not been
           completed yet. Nullable rather than zero so that next_run_at <= now
           excludes those rows instead of firing all of them on the first tick. */}}
    next_run_at BIGINT,
    last_run_at BIGINT,

    create_at BIGINT,
    update_at BIGINT,
    PRIMARY KEY (card_id)
) {{if .mysql}}DEFAULT CHARACTER SET utf8mb4{{end}};

{{- /* the scheduler's only hot query is "which active rows are due" */ -}}
{{- /* createIndexIfNeeded tableName columns */ -}}
{{ createIndexIfNeeded "recurring_cards" "active, next_run_at" }}
