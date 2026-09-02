// Copyright (c) 2015-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

package storetests

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mattermost/focalboard/server/model"
	"github.com/mattermost/focalboard/server/services/store"
)

func StoreTestRecurringCardsStore(t *testing.T, setup func(t *testing.T) (store.Store, func())) {
	t.Run("UpsertRecurringCard", func(t *testing.T) {
		store, tearDown := setup(t)
		defer tearDown()
		testUpsertRecurringCard(t, store)
	})

	t.Run("GetRecurringCard", func(t *testing.T) {
		store, tearDown := setup(t)
		defer tearDown()
		testGetRecurringCard(t, store)
	})

	t.Run("GetDueRecurringCards", func(t *testing.T) {
		store, tearDown := setup(t)
		defer tearDown()
		testGetDueRecurringCards(t, store)
	})

	t.Run("UpdateRecurringCardRun", func(t *testing.T) {
		store, tearDown := setup(t)
		defer tearDown()
		testUpdateRecurringCardRun(t, store)
	})

	t.Run("DeleteRecurringCard", func(t *testing.T) {
		store, tearDown := setup(t)
		defer tearDown()
		testDeleteRecurringCard(t, store)
	})
}

func millis(value int64) *int64 {
	return &value
}

func testRecurrenceConfig() *model.RecurrenceConfig {
	return &model.RecurrenceConfig{
		Enabled:         true,
		Mode:            model.RecurrenceModeSchedule,
		GroupPropertyID: "status-property-id",
		TargetOptionID:  "option-todo",
		Timezone:        "Europe/Moscow",
		Time:            "09:00",
		HistoryMode:     model.RecurrenceHistoryNewInstance,
		StartAt:         1767225600000,
		Rule: &model.RecurrenceRule{
			Kind:     model.RecurrenceRuleWeekly,
			Interval: 2,
			Weekdays: []int{1, 3, 5},
		},
	}
}

func testRecurringCard(cardID string, nextRunAt *int64) *model.RecurringCard {
	return &model.RecurringCard{
		CardID:    cardID,
		BoardID:   "board-id",
		Active:    true,
		Mode:      model.RecurrenceModeSchedule,
		Config:    testRecurrenceConfig(),
		NextRunAt: nextRunAt,
	}
}

func testUpsertRecurringCard(t *testing.T, s store.Store) {
	t.Run("insert a new row", func(t *testing.T) {
		rc := testRecurringCard("card-1", millis(1000))
		require.NoError(t, s.UpsertRecurringCard(rc))

		assert.NotZero(t, rc.CreateAt, "the store stamps create_at")
		assert.NotZero(t, rc.UpdateAt, "the store stamps update_at")

		stored, err := s.GetRecurringCard("card-1")
		require.NoError(t, err)
		assert.Equal(t, "board-id", stored.BoardID)
		assert.True(t, stored.Active)
		assert.Equal(t, model.RecurrenceModeSchedule, stored.Mode)
		assert.Equal(t, testRecurrenceConfig(), stored.Config)
		require.NotNil(t, stored.NextRunAt)
		assert.Equal(t, int64(1000), *stored.NextRunAt)
		assert.Nil(t, stored.LastRunAt, "a card that has never fired has no last run")
	})

	t.Run("replace an existing row without changing create_at", func(t *testing.T) {
		original, err := s.GetRecurringCard("card-1")
		require.NoError(t, err)

		updated := testRecurringCard("card-1", millis(5000))
		updated.Active = false
		updated.Mode = model.RecurrenceModeAfterDone
		updated.Config.Mode = model.RecurrenceModeAfterDone
		require.NoError(t, s.UpsertRecurringCard(updated))

		stored, err := s.GetRecurringCard("card-1")
		require.NoError(t, err)
		assert.Equal(t, original.CreateAt, stored.CreateAt, "create_at must survive an upsert")
		assert.False(t, stored.Active)
		assert.Equal(t, model.RecurrenceModeAfterDone, stored.Mode)
		require.NotNil(t, stored.NextRunAt)
		assert.Equal(t, int64(5000), *stored.NextRunAt)
	})

	t.Run("a nil next run round trips as not scheduled", func(t *testing.T) {
		require.NoError(t, s.UpsertRecurringCard(testRecurringCard("card-unscheduled", nil)))

		stored, err := s.GetRecurringCard("card-unscheduled")
		require.NoError(t, err)
		assert.Nil(t, stored.NextRunAt)
	})

	t.Run("a row without a card id or board id is rejected", func(t *testing.T) {
		require.Error(t, s.UpsertRecurringCard(&model.RecurringCard{BoardID: "board-id"}))
		require.Error(t, s.UpsertRecurringCard(&model.RecurringCard{CardID: "card-2"}))
	})
}

func testGetRecurringCard(t *testing.T, s store.Store) {
	t.Run("get an existing card", func(t *testing.T) {
		require.NoError(t, s.UpsertRecurringCard(testRecurringCard("card-1", millis(1000))))

		stored, err := s.GetRecurringCard("card-1")
		require.NoError(t, err)
		assert.Equal(t, "card-1", stored.CardID)
		require.NotNil(t, stored.Config)
		require.NotNil(t, stored.Config.Rule)
		assert.Equal(t, []int{1, 3, 5}, stored.Config.Rule.Weekdays, "the config survives the round trip intact")
	})

	t.Run("get a non-existent card", func(t *testing.T) {
		stored, err := s.GetRecurringCard("bogus")
		require.Error(t, err, "get non-existent recurring card should error")
		require.True(t, model.IsErrNotFound(err), "Should be ErrNotFound compatible error")
		require.Nil(t, stored)
	})
}

func testGetDueRecurringCards(t *testing.T, s store.Store) {
	overdue := testRecurringCard("card-overdue", millis(500))
	due := testRecurringCard("card-due", millis(1000))
	future := testRecurringCard("card-future", millis(9000))

	paused := testRecurringCard("card-paused", millis(100))
	paused.Active = false

	unscheduled := testRecurringCard("card-unscheduled", nil)
	unscheduled.Mode = model.RecurrenceModeAfterDone

	for _, rc := range []*model.RecurringCard{overdue, due, future, paused, unscheduled} {
		require.NoError(t, s.UpsertRecurringCard(rc))
	}

	t.Run("returns only what is due, soonest first", func(t *testing.T) {
		cards, err := s.GetDueRecurringCards(2000, 0)
		require.NoError(t, err)

		ids := make([]string, 0, len(cards))
		for _, card := range cards {
			ids = append(ids, card.CardID)
		}
		assert.Equal(t, []string{"card-overdue", "card-due"}, ids)
	})

	t.Run("a paused card is never due", func(t *testing.T) {
		cards, err := s.GetDueRecurringCards(9999, 0)
		require.NoError(t, err)

		for _, card := range cards {
			assert.NotEqual(t, "card-paused", card.CardID)
		}
	})

	t.Run("a card with no next run is never due", func(t *testing.T) {

		// The afterDone case: not scheduled until the card is completed. A zero
		// here instead of a null would make it due on every single tick.
		cards, err := s.GetDueRecurringCards(9999, 0)
		require.NoError(t, err)

		for _, card := range cards {
			assert.NotEqual(t, "card-unscheduled", card.CardID)
		}
	})

	t.Run("honours the limit", func(t *testing.T) {
		cards, err := s.GetDueRecurringCards(2000, 1)
		require.NoError(t, err)
		require.Len(t, cards, 1)
		assert.Equal(t, "card-overdue", cards[0].CardID, "the limit keeps the soonest")
	})

	t.Run("a limit of zero or less means no limit", func(t *testing.T) {
		cards, err := s.GetDueRecurringCards(9999, 0)
		require.NoError(t, err)
		assert.Len(t, cards, 3)

		cards, err = s.GetDueRecurringCards(9999, -1)
		require.NoError(t, err)
		assert.Len(t, cards, 3)
	})

	t.Run("nothing is due before the earliest run", func(t *testing.T) {
		cards, err := s.GetDueRecurringCards(1, 0)
		require.NoError(t, err)
		assert.Empty(t, cards)
	})
}

func testUpdateRecurringCardRun(t *testing.T, s store.Store) {
	t.Run("advances the schedule without touching the configuration", func(t *testing.T) {
		require.NoError(t, s.UpsertRecurringCard(testRecurringCard("card-1", millis(1000))))
		original, err := s.GetRecurringCard("card-1")
		require.NoError(t, err)

		require.NoError(t, s.UpdateRecurringCardRun("card-1", millis(7000), millis(1000)))

		stored, err := s.GetRecurringCard("card-1")
		require.NoError(t, err)
		require.NotNil(t, stored.NextRunAt)
		assert.Equal(t, int64(7000), *stored.NextRunAt)
		require.NotNil(t, stored.LastRunAt)
		assert.Equal(t, int64(1000), *stored.LastRunAt)

		// This is the point of the method: the scheduler firing a card must not
		// clobber a configuration saved concurrently by the REST layer.
		assert.Equal(t, original.Config, stored.Config)
		assert.Equal(t, original.Active, stored.Active)
		assert.Equal(t, original.Mode, stored.Mode)
		assert.Equal(t, original.BoardID, stored.BoardID)
		assert.Equal(t, original.CreateAt, stored.CreateAt)
	})

	t.Run("can put a card back into the not scheduled state", func(t *testing.T) {
		require.NoError(t, s.UpdateRecurringCardRun("card-1", nil, millis(7000)))

		stored, err := s.GetRecurringCard("card-1")
		require.NoError(t, err)
		assert.Nil(t, stored.NextRunAt)
		require.NotNil(t, stored.LastRunAt)
		assert.Equal(t, int64(7000), *stored.LastRunAt)
	})

	t.Run("update a non-existent card", func(t *testing.T) {
		err := s.UpdateRecurringCardRun("bogus", millis(1), nil)
		require.Error(t, err, "update of a non-existent recurring card should error")
		require.True(t, model.IsErrNotFound(err), "Should be ErrNotFound compatible error")
	})
}

func testDeleteRecurringCard(t *testing.T, s store.Store) {
	t.Run("delete a card", func(t *testing.T) {
		require.NoError(t, s.UpsertRecurringCard(testRecurringCard("card-1", millis(1000))))

		require.NoError(t, s.DeleteRecurringCard("card-1"))

		stored, err := s.GetRecurringCard("card-1")
		require.Error(t, err)
		require.True(t, model.IsErrNotFound(err), "Should be ErrNotFound compatible error")
		require.Nil(t, stored)
	})

	t.Run("delete a non-existent card", func(t *testing.T) {
		err := s.DeleteRecurringCard("bogus")
		require.Error(t, err, "delete of a non-existent recurring card should error")
		require.True(t, model.IsErrNotFound(err), "Should be ErrNotFound compatible error")
	})
}
