// Copyright (c) 2015-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

package notifyrecurring

import (
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mattermost/focalboard/server/model"
	"github.com/mattermost/focalboard/server/services/notify"
	"github.com/mattermost/focalboard/server/services/store/mockstore"

	"github.com/mattermost/mattermost/server/public/shared/mlog"
)

const (
	sourceCardID     = "card-source-id"
	occurrenceCardID = "card-occurrence-id"
	boardID          = "board-id"
	groupPropertyID  = "status-property-id"
	doneOptionID     = "option-done"
	targetOptionID   = "option-todo"
)

func setup(t *testing.T) (*Backend, *mockstore.MockStore) {
	t.Helper()

	ctrl := gomock.NewController(t)
	store := mockstore.NewMockStore(ctrl)
	logger := mlog.CreateConsoleTestLogger(t)

	return New(store, logger), store
}

func afterDoneConfig() *model.RecurrenceConfig {
	return &model.RecurrenceConfig{
		Enabled:         true,
		Mode:            model.RecurrenceModeAfterDone,
		GroupPropertyID: groupPropertyID,
		TargetOptionID:  targetOptionID,
		DoneOptionID:    doneOptionID,
		Timezone:        "Europe/Moscow",
		Time:            "09:00",
		HistoryMode:     model.RecurrenceHistoryNewInstance,
		DelayDays:       3,
	}
}

func scheduleConfig() *model.RecurrenceConfig {
	return &model.RecurrenceConfig{
		Enabled:         true,
		Mode:            model.RecurrenceModeSchedule,
		GroupPropertyID: groupPropertyID,
		TargetOptionID:  targetOptionID,
		Timezone:        "Europe/Moscow",
		Time:            "09:00",
		HistoryMode:     model.RecurrenceHistoryNewInstance,
		StartAt:         time.Now().UnixMilli(),
		Rule:            &model.RecurrenceRule{Kind: model.RecurrenceRuleDaily, Interval: 1},
	}
}

// sourceCard builds a card that carries the recurrence rule itself.
func sourceCard(t *testing.T, cfg *model.RecurrenceConfig, status string) *model.Block {
	t.Helper()

	fields := map[string]interface{}{
		"properties": map[string]interface{}{groupPropertyID: status},
	}
	require.NoError(t, model.SetCardRecurrenceFields(fields, model.CardTypeRecurring, cfg))

	return &model.Block{ID: sourceCardID, BoardID: boardID, Type: model.TypeCard, Fields: fields}
}

// occurrenceCard builds a card produced by a rule: no rule of its own, only a
// reference back to the card that has one.
func occurrenceCard(status string) *model.Block {
	return &model.Block{
		ID: occurrenceCardID, BoardID: boardID, Type: model.TypeCard,
		Fields: map[string]interface{}{
			"properties":                      map[string]interface{}{groupPropertyID: status},
			model.CardFieldRecurrenceSourceID: sourceCardID,
		},
	}
}

func plainCard(status string) *model.Block {
	return &model.Block{
		ID: "card-plain-id", BoardID: boardID, Type: model.TypeCard,
		Fields: map[string]interface{}{
			"properties": map[string]interface{}{groupPropertyID: status},
		},
	}
}

func update(oldCard, newCard *model.Block) notify.BlockChangeEvent {
	return notify.BlockChangeEvent{Action: notify.Update, TeamID: "team-id", BlockOld: oldCard, BlockChanged: newCard}
}

func existingRow(active bool) *model.RecurringCard {
	lastRun := int64(555)
	return &model.RecurringCard{
		CardID: sourceCardID, BoardID: boardID, Active: active,
		Mode: model.RecurrenceModeAfterDone, LastRunAt: &lastRun,
	}
}

func TestBlockChangedSchedulesOnDone(t *testing.T) {
	t.Run("moving a recurring card into the done column schedules the next occurrence", func(t *testing.T) {
		backend, store := setup(t)
		cfg := afterDoneConfig()

		store.EXPECT().GetRecurringCard(sourceCardID).Return(existingRow(true), nil)

		var nextRunAt, lastRunAt *int64
		store.EXPECT().UpdateRecurringCardRun(sourceCardID, gomock.Any(), gomock.Any()).
			DoAndReturn(func(_ string, next *int64, last *int64) error {
				nextRunAt, lastRunAt = next, last
				return nil
			})

		require.NoError(t, backend.BlockChanged(update(sourceCard(t, cfg, targetOptionID), sourceCard(t, cfg, doneOptionID))))

		require.NotNil(t, nextRunAt)
		assert.Greater(t, *nextRunAt, time.Now().UnixMilli(), "the next occurrence is in the future")

		// Completing a card does not produce an occurrence, so the record of when one
		// was last produced must not move.
		require.NotNil(t, lastRunAt)
		assert.Equal(t, int64(555), *lastRunAt)
	})

	t.Run("an edit while the card already sits in done does not reschedule", func(t *testing.T) {
		// The trigger is the transition, not the state. Without this the card would
		// be rescheduled on every keystroke while it sat in the done column.
		backend, _ := setup(t)
		cfg := afterDoneConfig()

		alreadyDone := sourceCard(t, cfg, doneOptionID)
		editedInDone := sourceCard(t, cfg, doneOptionID)
		editedInDone.Title = "a new title"

		// No store call is expected: gomock fails the test if one is made.
		require.NoError(t, backend.BlockChanged(update(alreadyDone, editedInDone)))
	})

	t.Run("a card that is not recurring is ignored", func(t *testing.T) {
		backend, _ := setup(t)

		require.NoError(t, backend.BlockChanged(update(plainCard(targetOptionID), plainCard(doneOptionID))))
	})

	t.Run("a recurring card in schedule mode is ignored", func(t *testing.T) {
		// A schedule recurrence is driven by its rule. Reaching the done column is
		// not what makes it fire, and must not reschedule it.
		backend, _ := setup(t)
		cfg := scheduleConfig()

		require.NoError(t, backend.BlockChanged(update(sourceCard(t, cfg, targetOptionID), sourceCard(t, cfg, doneOptionID))))
	})

	t.Run("a paused recurrence is ignored", func(t *testing.T) {
		backend, _ := setup(t)
		cfg := afterDoneConfig()
		cfg.Enabled = false

		require.NoError(t, backend.BlockChanged(update(sourceCard(t, cfg, targetOptionID), sourceCard(t, cfg, doneOptionID))))
	})

	t.Run("a block that is not a card is ignored", func(t *testing.T) {
		backend, _ := setup(t)

		text := &model.Block{ID: "text-id", BoardID: boardID, Type: model.TypeText}
		require.NoError(t, backend.BlockChanged(update(text, text)))
	})
}

func TestBlockChangedFollowsTheBackReference(t *testing.T) {
	t.Run("an occurrence reaching done schedules the source card, not itself", func(t *testing.T) {
		// This is what keeps an afterDone chain alive past its first iteration. The
		// source card never leaves the done column, so if the occurrence could not
		// point back at it, nothing would ever fire again.
		backend, store := setup(t)

		store.EXPECT().GetBlock(sourceCardID).Return(sourceCard(t, afterDoneConfig(), targetOptionID), nil)
		store.EXPECT().GetRecurringCard(sourceCardID).Return(existingRow(true), nil)

		var updatedCardID string
		var nextRunAt *int64
		store.EXPECT().UpdateRecurringCardRun(gomock.Any(), gomock.Any(), gomock.Any()).
			DoAndReturn(func(cardID string, next *int64, _ *int64) error {
				updatedCardID, nextRunAt = cardID, next
				return nil
			})

		require.NoError(t, backend.BlockChanged(update(occurrenceCard(targetOptionID), occurrenceCard(doneOptionID))))

		assert.Equal(t, sourceCardID, updatedCardID, "the source card's row is the one that carries the schedule")
		require.NotNil(t, nextRunAt)
	})

	t.Run("an occurrence whose source card is gone is reported, not acted on", func(t *testing.T) {
		backend, store := setup(t)

		store.EXPECT().GetBlock(sourceCardID).Return(nil, model.NewErrNotFound("block ID="+sourceCardID))

		require.NoError(t, backend.BlockChanged(update(occurrenceCard(targetOptionID), occurrenceCard(doneOptionID))))
	})

	t.Run("an occurrence whose source card is no longer recurring is reported, not acted on", func(t *testing.T) {
		backend, store := setup(t)

		store.EXPECT().GetBlock(sourceCardID).Return(plainCard(targetOptionID), nil)

		require.NoError(t, backend.BlockChanged(update(occurrenceCard(targetOptionID), occurrenceCard(doneOptionID))))
	})
}

func TestBlockChangedCancelsOnLeavingDone(t *testing.T) {
	t.Run("moving a card back out of done cancels the pending occurrence", func(t *testing.T) {
		backend, store := setup(t)
		cfg := afterDoneConfig()

		store.EXPECT().GetRecurringCard(sourceCardID).Return(existingRow(true), nil)

		var nextRunAt, lastRunAt *int64
		store.EXPECT().UpdateRecurringCardRun(sourceCardID, gomock.Any(), gomock.Any()).
			DoAndReturn(func(_ string, next *int64, last *int64) error {
				nextRunAt, lastRunAt = next, last
				return nil
			})

		require.NoError(t, backend.BlockChanged(update(sourceCard(t, cfg, doneOptionID), sourceCard(t, cfg, targetOptionID))))

		assert.Nil(t, nextRunAt, "undoing the completion undoes the occurrence it would have produced")
		require.NotNil(t, lastRunAt)
		assert.Equal(t, int64(555), *lastRunAt)
	})

	t.Run("an occurrence moved back out of done cancels on the source card", func(t *testing.T) {
		backend, store := setup(t)

		store.EXPECT().GetBlock(sourceCardID).Return(sourceCard(t, afterDoneConfig(), targetOptionID), nil)
		store.EXPECT().GetRecurringCard(sourceCardID).Return(existingRow(true), nil)
		store.EXPECT().UpdateRecurringCardRun(sourceCardID, nil, gomock.Any()).Return(nil)

		require.NoError(t, backend.BlockChanged(update(occurrenceCard(doneOptionID), occurrenceCard(targetOptionID))))
	})
}

func TestBlockChangedMissingRow(t *testing.T) {
	t.Run("a real transition with no row is reported and left alone", func(t *testing.T) {
		// By this point the change is known to be an enabled afterDone recurrence
		// making a real transition, so a missing row is drift between the card and
		// the index. Creating rows belongs elsewhere, so this only reports it.
		backend, store := setup(t)
		cfg := afterDoneConfig()

		store.EXPECT().GetRecurringCard(sourceCardID).
			Return(nil, model.NewErrNotFound("recurring card ID="+sourceCardID))

		require.NoError(t, backend.BlockChanged(update(sourceCard(t, cfg, targetOptionID), sourceCard(t, cfg, doneOptionID))))
	})
}

func TestBlockChangedRestoresUndeletedCard(t *testing.T) {
	t.Run("an undeleted recurring card has its recurrence reactivated", func(t *testing.T) {
		// An undelete arrives as an Add with no previous block, so it is recognised
		// by the card already having a row rather than by a change in DeleteAt.
		backend, store := setup(t)

		nextRun := int64(777)
		row := existingRow(false)
		row.NextRunAt = &nextRun
		store.EXPECT().GetRecurringCard(sourceCardID).Return(row, nil)

		var upserted *model.RecurringCard
		store.EXPECT().UpsertRecurringCard(gomock.Any()).
			DoAndReturn(func(rc *model.RecurringCard) error {
				upserted = rc
				return nil
			})

		err := backend.BlockChanged(notify.BlockChangeEvent{
			Action:       notify.Add,
			BlockChanged: sourceCard(t, afterDoneConfig(), targetOptionID),
		})
		require.NoError(t, err)

		require.NotNil(t, upserted)
		assert.True(t, upserted.Active)

		// An undelete is not a completion and must not reschedule anything.
		require.NotNil(t, upserted.NextRunAt)
		assert.Equal(t, int64(777), *upserted.NextRunAt)
	})

	t.Run("an already active row is left alone", func(t *testing.T) {
		backend, store := setup(t)

		store.EXPECT().GetRecurringCard(sourceCardID).Return(existingRow(true), nil)

		err := backend.BlockChanged(notify.BlockChangeEvent{
			Action:       notify.Add,
			BlockChanged: sourceCard(t, afterDoneConfig(), targetOptionID),
		})
		require.NoError(t, err)
	})

	t.Run("a newly created recurring card has no row yet and is left alone", func(t *testing.T) {
		backend, store := setup(t)

		store.EXPECT().GetRecurringCard(sourceCardID).
			Return(nil, model.NewErrNotFound("recurring card ID="+sourceCardID))

		err := backend.BlockChanged(notify.BlockChangeEvent{
			Action:       notify.Add,
			BlockChanged: sourceCard(t, afterDoneConfig(), targetOptionID),
		})
		require.NoError(t, err)
	})

	t.Run("a newly created plain card costs no store call", func(t *testing.T) {
		backend, _ := setup(t)

		err := backend.BlockChanged(notify.BlockChangeEvent{
			Action:       notify.Add,
			BlockChanged: plainCard(targetOptionID),
		})
		require.NoError(t, err)
	})
}
