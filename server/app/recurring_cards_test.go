// Copyright (c) 2015-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

package app

import (
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mattermost/focalboard/server/model"
)

const (
	recurringCardID  = "card-recurring-id"
	recurringBoardID = "board-recurring-id"
	occurrenceCardID = "card-occurrence-id"
	groupPropertyID  = "status-property-id"
	targetOptionID   = "option-todo"
	doneOptionID     = "option-done"
	recurringUserID  = "user-id-1"
)

func recurringTime(t *testing.T, year int, month time.Month, day, hour, minute int) int64 {
	t.Helper()

	loc, err := time.LoadLocation("Europe/Moscow")
	require.NoError(t, err)

	return time.Date(year, month, day, hour, minute, 0, 0, loc).UnixMilli()
}

func scheduleConfig(t *testing.T) *model.RecurrenceConfig {
	t.Helper()

	return &model.RecurrenceConfig{
		Enabled:         true,
		Mode:            model.RecurrenceModeSchedule,
		GroupPropertyID: groupPropertyID,
		TargetOptionID:  targetOptionID,
		Timezone:        "Europe/Moscow",
		Time:            "09:00",
		HistoryMode:     model.RecurrenceHistoryNewInstance,
		StartAt:         recurringTime(t, 2026, 5, 1, 0, 0),
		Rule: &model.RecurrenceRule{
			Kind:     model.RecurrenceRuleDaily,
			Interval: 1,
		},
	}
}

// recurringCardBlock builds a source card carrying the recurrence fields exactly as
// the server stores them, by going through the same helper the store uses.
func recurringCardBlock(t *testing.T, cfg *model.RecurrenceConfig) *model.Block {
	t.Helper()

	fields := map[string]interface{}{
		"icon":         "🪴",
		"contentOrder": []interface{}{},
		"isTemplate":   false,
		"properties": map[string]interface{}{
			groupPropertyID: doneOptionID,
			"other-prop":    "untouched",
		},
	}
	require.NoError(t, model.SetCardRecurrenceFields(fields, model.CardTypeRecurring, cfg))

	return &model.Block{
		ID:        recurringCardID,
		ParentID:  recurringBoardID,
		BoardID:   recurringBoardID,
		CreatedBy: recurringUserID,
		Type:      model.TypeCard,
		Title:     "Water the plants",
		Fields:    fields,
	}
}

func recurringBoard() *model.Board {
	return &model.Board{
		ID:     recurringBoardID,
		TeamID: "team-id",
		CardProperties: []map[string]interface{}{
			{
				"id":   groupPropertyID,
				"name": "Status",
				"type": "select",
				"options": []interface{}{
					map[string]interface{}{"id": targetOptionID, "value": "To do"},
					map[string]interface{}{"id": doneOptionID, "value": "Done"},
				},
			},
		},
	}
}

func recurringRow(t *testing.T, cfg *model.RecurrenceConfig, nextRunAt int64) *model.RecurringCard {
	t.Helper()

	next := nextRunAt
	return &model.RecurringCard{
		CardID:    recurringCardID,
		BoardID:   recurringBoardID,
		Active:    true,
		Mode:      cfg.Mode,
		Config:    cfg,
		NextRunAt: &next,
		CreateAt:  1,
		UpdateAt:  1,
	}
}

// expectBroadcasts allows the calls the websocket broadcast and file-copy paths make.
// They run on the block change notifier, so their timing is not deterministic and
// they are not what these tests are asserting.
func expectBroadcasts(th *TestHelper) {
	th.Store.EXPECT().GetMembersForBoard(gomock.Any()).Return([]*model.BoardMember{}, nil).AnyTimes()
	th.Store.EXPECT().GetBoard(recurringBoardID).Return(recurringBoard(), nil).AnyTimes()
}

func TestProcessDueRecurringCards(t *testing.T) {
	t.Run("does nothing when no card is due", func(t *testing.T) {
		th, tearDown := SetupTestHelper(t)
		defer tearDown()

		th.Store.EXPECT().GetDueRecurringCards(gomock.Any(), maxRecurringCardsPerTick).
			Return([]*model.RecurringCard{}, nil)

		require.NoError(t, th.App.ProcessDueRecurringCards())
	})

	t.Run("newInstance creates an occurrence in the target column", func(t *testing.T) {
		th, tearDown := SetupTestHelper(t)
		defer tearDown()
		expectBroadcasts(th)

		cfg := scheduleConfig(t)
		card := recurringCardBlock(t, cfg)
		occurrence := &model.Block{ID: occurrenceCardID, BoardID: recurringBoardID, Type: model.TypeCard}

		th.Store.EXPECT().GetDueRecurringCards(gomock.Any(), maxRecurringCardsPerTick).
			Return([]*model.RecurringCard{recurringRow(t, cfg, recurringTime(t, 2026, 5, 10, 9, 0))}, nil)
		th.Store.EXPECT().GetBlock(recurringCardID).Return(card, nil).AnyTimes()
		th.Store.EXPECT().GetBlock(occurrenceCardID).Return(occurrence, nil).AnyTimes()
		th.Store.EXPECT().UpdateRecurringCardRun(recurringCardID, gomock.Any(), gomock.Any()).Return(nil)
		th.Store.EXPECT().DuplicateBlock(recurringBoardID, recurringCardID, recurringUserID, false).
			Return([]*model.Block{occurrence}, nil)

		var patch *model.BlockPatch
		th.Store.EXPECT().PatchBlock(occurrenceCardID, gomock.Any(), recurringUserID).
			DoAndReturn(func(_ string, p *model.BlockPatch, _ string) error {
				patch = p
				return nil
			})

		require.NoError(t, th.App.ProcessDueRecurringCards())

		require.NotNil(t, patch)
		properties, ok := patch.UpdatedFields["properties"].(map[string]interface{})
		require.True(t, ok)
		assert.Equal(t, targetOptionID, properties[groupPropertyID], "the occurrence lands in the target column")
		assert.Equal(t, "untouched", properties["other-prop"], "unrelated properties are carried over")
	})

	t.Run("the new occurrence is not itself recurring", func(t *testing.T) {
		// The worst failure available here. An occurrence that is still marked
		// recurring would spawn its own occurrences, and the board would fill
		// geometrically. Both keys must be absent, not merely disabled.
		th, tearDown := SetupTestHelper(t)
		defer tearDown()
		expectBroadcasts(th)

		cfg := scheduleConfig(t)
		card := recurringCardBlock(t, cfg)
		occurrence := &model.Block{ID: occurrenceCardID, BoardID: recurringBoardID, Type: model.TypeCard}

		th.Store.EXPECT().GetDueRecurringCards(gomock.Any(), maxRecurringCardsPerTick).
			Return([]*model.RecurringCard{recurringRow(t, cfg, recurringTime(t, 2026, 5, 10, 9, 0))}, nil)
		th.Store.EXPECT().GetBlock(recurringCardID).Return(card, nil).AnyTimes()
		th.Store.EXPECT().GetBlock(occurrenceCardID).Return(occurrence, nil).AnyTimes()
		th.Store.EXPECT().UpdateRecurringCardRun(recurringCardID, gomock.Any(), gomock.Any()).Return(nil)
		th.Store.EXPECT().DuplicateBlock(recurringBoardID, recurringCardID, recurringUserID, false).
			Return([]*model.Block{occurrence}, nil)

		var patch *model.BlockPatch
		th.Store.EXPECT().PatchBlock(occurrenceCardID, gomock.Any(), recurringUserID).
			DoAndReturn(func(_ string, p *model.BlockPatch, _ string) error {
				patch = p
				return nil
			})

		require.NoError(t, th.App.ProcessDueRecurringCards())

		require.NotNil(t, patch)
		assert.Contains(t, patch.DeletedFields, model.CardFieldCardType)
		assert.Contains(t, patch.DeletedFields, model.CardFieldRecurrence)

		// And prove the patch really removes them, rather than trusting the names.
		applied := &model.Block{Fields: map[string]interface{}{}}
		for key, value := range card.Fields {
			applied.Fields[key] = value
		}
		patch.Patch(applied)

		assert.NotContains(t, applied.Fields, model.CardFieldCardType)
		assert.NotContains(t, applied.Fields, model.CardFieldRecurrence)

		cardType, occurrenceCfg, err := model.CardRecurrenceFromFields(applied.Fields)
		require.NoError(t, err)
		assert.Equal(t, model.CardTypeNormal, cardType)
		assert.Nil(t, occurrenceCfg)
		assert.False(t, model.IsRecurrenceActive(cardType, occurrenceCfg))
	})

	t.Run("returnSame moves the source card instead of copying it", func(t *testing.T) {
		th, tearDown := SetupTestHelper(t)
		defer tearDown()
		expectBroadcasts(th)

		cfg := scheduleConfig(t)
		cfg.HistoryMode = model.RecurrenceHistoryReturnSame
		card := recurringCardBlock(t, cfg)

		th.Store.EXPECT().GetDueRecurringCards(gomock.Any(), maxRecurringCardsPerTick).
			Return([]*model.RecurringCard{recurringRow(t, cfg, recurringTime(t, 2026, 5, 10, 9, 0))}, nil)
		th.Store.EXPECT().GetBlock(recurringCardID).Return(card, nil).AnyTimes()
		th.Store.EXPECT().UpdateRecurringCardRun(recurringCardID, gomock.Any(), gomock.Any()).Return(nil)

		var patch *model.BlockPatch
		th.Store.EXPECT().PatchBlock(recurringCardID, gomock.Any(), recurringUserID).
			DoAndReturn(func(_ string, p *model.BlockPatch, _ string) error {
				patch = p
				return nil
			})

		require.NoError(t, th.App.ProcessDueRecurringCards())

		require.NotNil(t, patch)
		properties, ok := patch.UpdatedFields["properties"].(map[string]interface{})
		require.True(t, ok)
		assert.Equal(t, targetOptionID, properties[groupPropertyID])

		// The source card keeps its rule: returnSame moves the card, it does not
		// strip the recurrence from it.
		assert.Empty(t, patch.DeletedFields)
	})

	t.Run("an outage of several days produces exactly one card", func(t *testing.T) {
		th, tearDown := SetupTestHelper(t)
		defer tearDown()
		expectBroadcasts(th)

		cfg := scheduleConfig(t)
		card := recurringCardBlock(t, cfg)
		occurrence := &model.Block{ID: occurrenceCardID, BoardID: recurringBoardID, Type: model.TypeCard}

		// Last fired on 10 May; the process only comes back several days later.
		missed := recurringTime(t, 2026, 5, 10, 9, 0)

		th.Store.EXPECT().GetDueRecurringCards(gomock.Any(), maxRecurringCardsPerTick).
			Return([]*model.RecurringCard{recurringRow(t, cfg, missed)}, nil)
		th.Store.EXPECT().GetBlock(recurringCardID).Return(card, nil).AnyTimes()
		th.Store.EXPECT().GetBlock(occurrenceCardID).Return(occurrence, nil).AnyTimes()

		var nextRunAt *int64
		// Times(1) is the assertion: one advance, not one per missed period.
		th.Store.EXPECT().UpdateRecurringCardRun(recurringCardID, gomock.Any(), gomock.Any()).
			DoAndReturn(func(_ string, next *int64, last *int64) error {
				nextRunAt = next
				require.NotNil(t, last)
				return nil
			}).Times(1)

		// And exactly one card, not one per missed period.
		th.Store.EXPECT().DuplicateBlock(recurringBoardID, recurringCardID, recurringUserID, false).
			Return([]*model.Block{occurrence}, nil).Times(1)
		th.Store.EXPECT().PatchBlock(occurrenceCardID, gomock.Any(), recurringUserID).Return(nil).Times(1)

		require.NoError(t, th.App.ProcessDueRecurringCards())

		require.NotNil(t, nextRunAt)
		assert.Greater(t, *nextRunAt, missed, "the schedule moved forward")
		assert.Greater(t, *nextRunAt, time.Now().UnixMilli(), "and landed in the future")
	})

	t.Run("a disabled rule deactivates the row and keeps last_run_at", func(t *testing.T) {
		th, tearDown := SetupTestHelper(t)
		defer tearDown()

		cfg := scheduleConfig(t)
		cfg.Enabled = false
		card := recurringCardBlock(t, cfg)

		row := recurringRow(t, cfg, recurringTime(t, 2026, 5, 10, 9, 0))
		lastRun := recurringTime(t, 2026, 5, 9, 9, 0)
		row.LastRunAt = &lastRun

		th.Store.EXPECT().GetDueRecurringCards(gomock.Any(), maxRecurringCardsPerTick).
			Return([]*model.RecurringCard{row}, nil)
		th.Store.EXPECT().GetBlock(recurringCardID).Return(card, nil)

		var upserted *model.RecurringCard
		th.Store.EXPECT().UpsertRecurringCard(gomock.Any()).
			DoAndReturn(func(rc *model.RecurringCard) error {
				upserted = rc
				return nil
			})

		require.NoError(t, th.App.ProcessDueRecurringCards())

		require.NotNil(t, upserted)
		assert.False(t, upserted.Active)
		require.NotNil(t, upserted.LastRunAt, "a pause must not discard when the card last fired")
		assert.Equal(t, lastRun, *upserted.LastRunAt)
	})

	t.Run("a source card that no longer exists drops the row", func(t *testing.T) {
		th, tearDown := SetupTestHelper(t)
		defer tearDown()

		cfg := scheduleConfig(t)

		th.Store.EXPECT().GetDueRecurringCards(gomock.Any(), maxRecurringCardsPerTick).
			Return([]*model.RecurringCard{recurringRow(t, cfg, recurringTime(t, 2026, 5, 10, 9, 0))}, nil)
		th.Store.EXPECT().GetBlock(recurringCardID).
			Return(nil, model.NewErrNotFound("block ID="+recurringCardID))
		th.Store.EXPECT().DeleteRecurringCard(recurringCardID).Return(nil)

		require.NoError(t, th.App.ProcessDueRecurringCards())
	})

	t.Run("a soft-deleted source card deactivates the row rather than dropping it", func(t *testing.T) {
		// Soft deletion is reversible: an undeleted card comes back still carrying
		// its recurrence fields, so the row has to survive for it to be restored.
		th, tearDown := SetupTestHelper(t)
		defer tearDown()

		cfg := scheduleConfig(t)
		card := recurringCardBlock(t, cfg)
		card.DeleteAt = 1234

		th.Store.EXPECT().GetDueRecurringCards(gomock.Any(), maxRecurringCardsPerTick).
			Return([]*model.RecurringCard{recurringRow(t, cfg, recurringTime(t, 2026, 5, 10, 9, 0))}, nil)
		th.Store.EXPECT().GetBlock(recurringCardID).Return(card, nil)

		var upserted *model.RecurringCard
		th.Store.EXPECT().UpsertRecurringCard(gomock.Any()).
			DoAndReturn(func(rc *model.RecurringCard) error {
				upserted = rc
				return nil
			})

		require.NoError(t, th.App.ProcessDueRecurringCards())

		require.NotNil(t, upserted)
		assert.False(t, upserted.Active)
		require.NotNil(t, upserted.Config, "the configuration is kept so an undelete can resume it")
	})

	t.Run("a target column that no longer exists skips without advancing", func(t *testing.T) {
		// Not advancing is what makes this self-healing: fixing the configuration
		// is enough to make the card fire again on the next tick.
		th, tearDown := SetupTestHelper(t)
		defer tearDown()

		cfg := scheduleConfig(t)
		cfg.TargetOptionID = "option-that-was-deleted"
		card := recurringCardBlock(t, cfg)

		th.Store.EXPECT().GetDueRecurringCards(gomock.Any(), maxRecurringCardsPerTick).
			Return([]*model.RecurringCard{recurringRow(t, cfg, recurringTime(t, 2026, 5, 10, 9, 0))}, nil)
		th.Store.EXPECT().GetBlock(recurringCardID).Return(card, nil)
		th.Store.EXPECT().GetBoard(recurringBoardID).Return(recurringBoard(), nil)

		// No UpdateRecurringCardRun and no DuplicateBlock are expected: gomock fails
		// the test if either is called.
		require.NoError(t, th.App.ProcessDueRecurringCards())
	})

	t.Run("a group property that no longer exists skips without advancing", func(t *testing.T) {
		th, tearDown := SetupTestHelper(t)
		defer tearDown()

		cfg := scheduleConfig(t)
		cfg.GroupPropertyID = "property-that-was-deleted"
		card := recurringCardBlock(t, cfg)

		th.Store.EXPECT().GetDueRecurringCards(gomock.Any(), maxRecurringCardsPerTick).
			Return([]*model.RecurringCard{recurringRow(t, cfg, recurringTime(t, 2026, 5, 10, 9, 0))}, nil)
		th.Store.EXPECT().GetBlock(recurringCardID).Return(card, nil)
		th.Store.EXPECT().GetBoard(recurringBoardID).Return(recurringBoard(), nil)

		require.NoError(t, th.App.ProcessDueRecurringCards())
	})

	t.Run("a failing row does not abort the rest of the batch", func(t *testing.T) {
		th, tearDown := SetupTestHelper(t)
		defer tearDown()
		expectBroadcasts(th)

		cfg := scheduleConfig(t)
		card := recurringCardBlock(t, cfg)
		occurrence := &model.Block{ID: occurrenceCardID, BoardID: recurringBoardID, Type: model.TypeCard}

		broken := recurringRow(t, cfg, recurringTime(t, 2026, 5, 10, 9, 0))
		broken.CardID = "card-broken-id"

		th.Store.EXPECT().GetDueRecurringCards(gomock.Any(), maxRecurringCardsPerTick).
			Return([]*model.RecurringCard{broken, recurringRow(t, cfg, recurringTime(t, 2026, 5, 10, 9, 0))}, nil)

		// The first row fails on a store error that is not a not-found.
		th.Store.EXPECT().GetBlock("card-broken-id").Return(nil, blockError{"database is on fire"})

		// The second row must still be processed to completion.
		th.Store.EXPECT().GetBlock(recurringCardID).Return(card, nil).AnyTimes()
		th.Store.EXPECT().GetBlock(occurrenceCardID).Return(occurrence, nil).AnyTimes()
		th.Store.EXPECT().UpdateRecurringCardRun(recurringCardID, gomock.Any(), gomock.Any()).Return(nil)
		th.Store.EXPECT().DuplicateBlock(recurringBoardID, recurringCardID, recurringUserID, false).
			Return([]*model.Block{occurrence}, nil)
		th.Store.EXPECT().PatchBlock(occurrenceCardID, gomock.Any(), recurringUserID).Return(nil)

		require.NoError(t, th.App.ProcessDueRecurringCards(), "one bad row must not fail the tick")
	})

	t.Run("a failed occurrence restores the previous schedule", func(t *testing.T) {
		th, tearDown := SetupTestHelper(t)
		defer tearDown()
		expectBroadcasts(th)

		cfg := scheduleConfig(t)
		card := recurringCardBlock(t, cfg)
		missed := recurringTime(t, 2026, 5, 10, 9, 0)
		row := recurringRow(t, cfg, missed)

		th.Store.EXPECT().GetDueRecurringCards(gomock.Any(), maxRecurringCardsPerTick).
			Return([]*model.RecurringCard{row}, nil)
		th.Store.EXPECT().GetBlock(recurringCardID).Return(card, nil).AnyTimes()

		gomock.InOrder(
			// The advance.
			th.Store.EXPECT().UpdateRecurringCardRun(recurringCardID, gomock.Any(), gomock.Any()).Return(nil),
			// Materialising fails.
			th.Store.EXPECT().DuplicateBlock(recurringBoardID, recurringCardID, recurringUserID, false).
				Return(nil, blockError{"cannot duplicate"}),
			// So the previous scheduling state is put back and the occurrence is
			// retried on the next tick instead of being consumed.
			th.Store.EXPECT().UpdateRecurringCardRun(recurringCardID, &missed, row.LastRunAt).Return(nil),
		)

		require.NoError(t, th.App.ProcessDueRecurringCards())
	})
}
