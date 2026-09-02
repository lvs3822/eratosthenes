{{- /* Dropping the table drops idx_recurring_cards_active_next_run_at with it on
      all three engines. No data is lost that cannot be rebuilt: every row is
      derived from a card block's fields.recurrence. */ -}}

DROP TABLE IF EXISTS {{.prefix}}recurring_cards;
