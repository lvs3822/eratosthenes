package migrationstests

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func Test41CreateRecurringCardsTable(t *testing.T) {
	t.Run("creates the table and its index", func(t *testing.T) {
		th, tearDown := SetupTestHelper(t)
		defer tearDown()

		th.f.MigrateToStep(41)

		var tables, indexes int

		switch {
		case th.IsSQLite():
			require.NoError(t, th.f.DB().Get(&tables,
				"SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'focalboard_recurring_cards'"))
			require.NoError(t, th.f.DB().Get(&indexes,
				"SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name = 'idx_recurring_cards_active_next_run_at'"))
		case th.IsMySQL():
			require.NoError(t, th.f.DB().Get(&tables,
				"SELECT COUNT(*) FROM INFORMATION_SCHEMA.TABLES "+
					"WHERE table_schema = DATABASE() AND table_name = 'focalboard_recurring_cards'"))
			// STATISTICS holds one row per indexed column, so count the index once.
			require.NoError(t, th.f.DB().Get(&indexes,
				"SELECT COUNT(DISTINCT index_name) FROM INFORMATION_SCHEMA.STATISTICS "+
					"WHERE table_schema = DATABASE() AND table_name = 'focalboard_recurring_cards' "+
					"AND index_name = 'idx_recurring_cards_active_next_run_at'"))
		case th.IsPostgres():
			require.NoError(t, th.f.DB().Get(&tables,
				"SELECT COUNT(*) FROM information_schema.tables WHERE table_name = 'focalboard_recurring_cards'"))
			require.NoError(t, th.f.DB().Get(&indexes,
				"SELECT COUNT(*) FROM pg_indexes WHERE tablename = 'focalboard_recurring_cards' "+
					"AND indexname = 'idx_recurring_cards_active_next_run_at'"))
		}

		require.Equal(t, 1, tables)
		require.Equal(t, 1, indexes)
	})

	t.Run("the due query skips paused rows and rows that are not scheduled", func(t *testing.T) {
		// next_run_at is nullable on purpose: an afterDone card has no scheduled
		// time until it is completed. Storing zero instead would make every such
		// card due on the scheduler's first tick. This asserts the three-valued
		// logic behaves the same on all three engines.
		th, tearDown := SetupTestHelper(t)
		defer tearDown()

		th.f.MigrateToStep(41)

		_, err := th.f.DB().Exec(
			"INSERT INTO focalboard_recurring_cards " +
				"(card_id, board_id, active, mode, config, next_run_at, last_run_at, create_at, update_at) VALUES " +
				"('card-due', 'board-id', true, 'schedule', '{\"enabled\":true}', 1000, NULL, 1, 1), " +
				"('card-future', 'board-id', true, 'schedule', '{\"enabled\":true}', 9000, NULL, 1, 1), " +
				"('card-paused', 'board-id', false, 'schedule', '{\"enabled\":false}', 1000, NULL, 1, 1), " +
				"('card-after-done', 'board-id', true, 'afterDone', '{\"enabled\":true}', NULL, NULL, 1, 1)")
		require.NoError(t, err)

		var due []string
		require.NoError(t, th.f.DB().Select(&due,
			"SELECT card_id FROM focalboard_recurring_cards "+
				"WHERE active = true AND next_run_at <= 2000 ORDER BY card_id"))

		require.Equal(t, []string{"card-due"}, due)
	})
}
