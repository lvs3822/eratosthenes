// Copyright (c) 2015-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

package app

import (
	"errors"
	"fmt"

	"github.com/mattermost/focalboard/server/model"
	"github.com/mattermost/focalboard/server/utils"

	"github.com/mattermost/mattermost/server/public/shared/mlog"
)

const (
	// maxRecurringCardsPerTick caps how many rows a single tick will act on. This
	// is a blast radius limit rather than a load limit: a personal board will never
	// approach it, but if a bug ever leaves many rows with next_run_at in the past,
	// a cap turns a flood of cards into a slow drip that is visible in the log and
	// can be stopped.
	maxRecurringCardsPerTick = 100

	// maxCatchUpPeriods bounds the walk that advances next_run_at past the periods
	// missed while the server was down. The daily rule is the finest granularity
	// available, so this is roughly eleven years of downtime. It exists only so a
	// configuration that somehow slipped past validation cannot spin forever.
	maxCatchUpPeriods = 4000
)

var (
	// ErrCatchUpDidNotConverge means the walk past missed periods hit its bound.
	// NextRunAt is contractually strictly increasing, so a valid configuration
	// cannot produce this.
	ErrCatchUpDidNotConverge = errors.New("advancing the recurrence past the missed periods did not reach the future")

	// ErrNoOccurrenceCreated means duplicating the source card returned nothing.
	ErrNoOccurrenceCreated = errors.New("duplicating the source card produced no blocks")
)

// ProcessDueRecurringCards materialises every recurring card whose next occurrence
// has come due, and is the entry point a scheduler calls on a timer.
//
// An error on one row is logged and the batch continues, so a single broken
// configuration cannot stop every other card on the instance from recurring.
func (a *App) ProcessDueRecurringCards() error {
	now := utils.GetMillis()

	rows, err := a.store.GetDueRecurringCards(now, maxRecurringCardsPerTick)
	if err != nil {
		return fmt.Errorf("cannot fetch the due recurring cards: %w", err)
	}

	if len(rows) == 0 {
		return nil
	}

	if len(rows) == maxRecurringCardsPerTick {
		a.logger.Warn("a recurring cards tick returned a full batch, which usually means something is wrong",
			mlog.Int("count", len(rows)))
	}

	for _, row := range rows {
		if procErr := a.processDueRecurringCard(row, now); procErr != nil {
			a.logRecurrenceProblem(row.CardID, procErr)
			continue
		}
		a.clearRecurrenceProblem(row.CardID)
	}

	return nil
}

// processDueRecurringCard reconciles one row against its source card and, if the
// card still wants to recur, materialises the occurrence.
func (a *App) processDueRecurringCard(row *model.RecurringCard, now int64) error {
	card, err := a.store.GetBlock(row.CardID)
	if model.IsErrNotFound(err) {
		// The only unrecoverable case. Everything else keeps the row, because
		// everything else can be undone by the user.
		return a.store.DeleteRecurringCard(row.CardID)
	}
	if err != nil {
		return err
	}

	cardType, cfg, fieldsErr := model.CardRecurrenceFromFields(card.Fields)

	// A soft delete is reversible: UndeleteBlock restores the block from
	// blocks_history with its fields intact, so an undeleted card comes back still
	// carrying cardType and recurrence. Dropping the row here would make recurrence
	// stop working silently after an ordinary undo, with nothing in the interface
	// to say why.
	if card.DeleteAt != 0 {
		return a.deactivateRecurringCard(row, cfg)
	}

	if fieldsErr != nil {
		return fieldsErr
	}

	if cardType != model.CardTypeRecurring || cfg == nil {
		return a.store.DeleteRecurringCard(row.CardID)
	}

	if !cfg.Enabled {
		// Reaching this branch means the table has drifted from the card, because
		// GetDueRecurringCards filters on active, and active holds the conjunction
		// of cardType and enabled. Deleting the row here would discard last_run_at
		// to punish a bookkeeping failure, so reconcile instead.
		return a.deactivateRecurringCard(row, cfg)
	}

	return a.materialiseRecurringCard(row, card, cfg, now)
}

// deactivateRecurringCard clears the active flag while keeping the row, so that
// last_run_at survives a pause and a card that comes back can be resumed.
func (a *App) deactivateRecurringCard(row *model.RecurringCard, cfg *model.RecurrenceConfig) error {
	reconciled := *row
	reconciled.Active = false

	// Refresh from the card when it could be read: this path exists because the two
	// had drifted apart, and the card is the source of truth.
	if cfg != nil {
		reconciled.Config = cfg
		reconciled.Mode = cfg.Mode
	}

	return a.store.UpsertRecurringCard(&reconciled)
}

// materialiseRecurringCard advances the schedule and then produces the occurrence.
//
// The order is deliberate and cannot be replaced by a transaction: the store
// exposes no cross-method transaction, and the generated wrappers skip
// transactions entirely on SQLite. Advancing first means an ordinary failure is
// compensated below and retried on the next tick, while a hard process kill in the
// sub-second window between the two writes loses exactly one occurrence. The
// reverse order would instead duplicate a card every time the advance failed,
// which on SQLite is a routine busy-timeout error rather than a rare one.
func (a *App) materialiseRecurringCard(row *model.RecurringCard, card *model.Block, cfg *model.RecurrenceConfig, now int64) error {
	board, err := a.store.GetBoard(card.BoardID)
	if err != nil {
		return err
	}

	// Checked before anything is written, and reported without advancing, so that
	// fixing the configuration is enough to make the card fire again.
	if err := checkRecurrenceTarget(board, cfg); err != nil {
		return err
	}

	nextRunAt, err := nextRunAfterCatchUp(cfg, row, now)
	if err != nil {
		return err
	}

	firedAt := now
	if err := a.store.UpdateRecurringCardRun(row.CardID, nextRunAt, &firedAt); err != nil {
		return err
	}

	if err := a.produceOccurrence(card, cfg); err != nil {
		// Compensate, so an ordinary failure does not consume the occurrence.
		if restoreErr := a.store.UpdateRecurringCardRun(row.CardID, row.NextRunAt, row.LastRunAt); restoreErr != nil {
			a.logger.Error("cannot restore the schedule of a recurring card after a failed occurrence",
				mlog.String("cardID", row.CardID), mlog.Err(restoreErr))
		}
		return err
	}

	return nil
}

// nextRunAfterCatchUp returns when the card should next fire.
//
// If the server was down for several periods, exactly one occurrence is produced
// and the schedule is walked forward until it is in the future, rather than one
// card per missed period. NextRunAt is contractually strictly greater than its
// argument, which is what makes the walk terminate.
func nextRunAfterCatchUp(cfg *model.RecurrenceConfig, row *model.RecurringCard, now int64) (*int64, error) {
	// "afterDone" has no period to advance by: the next occurrence is scheduled
	// when the card is next completed, not on a cycle. The walk below deliberately
	// does not apply in this mode; a null puts the row back into the not-scheduled
	// state until the done-column trigger sets it again.
	if cfg.Mode == model.RecurrenceModeAfterDone {
		return nil, nil
	}

	if row.NextRunAt == nil {
		return nil, model.ErrInvalidRecurrence{
			Field:  "nextRunAt",
			Reason: "a scheduled card reached the scheduler with no next run",
		}
	}

	next := *row.NextRunAt
	for i := 0; i < maxCatchUpPeriods; i++ {
		candidate, err := model.NextRunAt(cfg, model.GetTimeForMillis(next))
		if err != nil {
			return nil, err
		}

		next = candidate
		if next > now {
			return &next, nil
		}
	}

	return nil, ErrCatchUpDidNotConverge
}

func (a *App) produceOccurrence(card *model.Block, cfg *model.RecurrenceConfig) error {
	switch cfg.HistoryMode {
	case model.RecurrenceHistoryNewInstance:
		return a.createRecurringOccurrence(card, cfg)
	case model.RecurrenceHistoryReturnSame:
		return a.returnRecurringCard(card, cfg)
	default:
		return model.ErrInvalidRecurrence{
			Field:  model.RecurrenceFieldHistoryMode,
			Reason: fmt.Sprintf("%q is not a history mode this server can materialise", cfg.HistoryMode),
		}
	}
}

// createRecurringOccurrence copies the source card into the target column.
//
// DuplicateBlock brings the title, icon and content blocks across and copies the
// attached files, and skips comment blocks, so the new occurrence starts a fresh
// discussion. It copies the root block's fields verbatim, though, so the copy
// arrives still marked as recurring; the patch below strips that in the same write
// that sets the target column. A new occurrence left marked recurring would spawn
// its own children, and the board would fill geometrically.
func (a *App) createRecurringOccurrence(card *model.Block, cfg *model.RecurrenceConfig) error {
	blocks, err := a.DuplicateBlock(card.BoardID, card.ID, card.CreatedBy, false)
	if err != nil {
		return err
	}

	if len(blocks) == 0 {
		return ErrNoOccurrenceCreated
	}

	// duplicateBlock puts the root block first.
	occurrence := blocks[0]

	patch := &model.BlockPatch{
		UpdatedFields: map[string]interface{}{
			"properties": targetProperties(card, cfg),
		},
		DeletedFields: []string{model.CardFieldCardType, model.CardFieldRecurrence},
	}

	// The websocket broadcast happens regardless of disableNotify; only the
	// notification service is suppressed, so open clients still update but nobody
	// is pinged by a card the scheduler created.
	if _, err := a.PatchBlockAndNotify(occurrence.ID, patch, card.CreatedBy, true); err != nil {
		return err
	}

	return nil
}

// returnRecurringCard moves the source card itself back into the target column.
func (a *App) returnRecurringCard(card *model.Block, cfg *model.RecurrenceConfig) error {
	patch := &model.BlockPatch{
		UpdatedFields: map[string]interface{}{
			"properties": targetProperties(card, cfg),
		},
	}

	if _, err := a.PatchBlockAndNotify(card.ID, patch, card.CreatedBy, true); err != nil {
		return err
	}

	return nil
}

// targetProperties copies the source card's properties and moves the group-by
// property to the target column.
//
// The value is a plain string because a group-by property is always single valued.
// A multiSelect cannot be a group-by property through the interface at all, so an
// array here could only arrive from a hand-edited configuration.
func targetProperties(card *model.Block, cfg *model.RecurrenceConfig) map[string]interface{} {
	properties := make(map[string]interface{})

	if raw, ok := card.Fields["properties"]; ok {
		if existing, isMap := raw.(map[string]interface{}); isMap {
			for key, value := range existing {
				properties[key] = value
			}
		}
	}

	properties[cfg.GroupPropertyID] = cfg.TargetOptionID

	return properties
}

// checkRecurrenceTarget reports whether the configured column still exists on the
// board. The model layer cannot do this because it has no board, so it is checked
// here, before anything is written.
func checkRecurrenceTarget(board *model.Board, cfg *model.RecurrenceConfig) error {
	for _, property := range board.CardProperties {
		propertyID, _ := property["id"].(string)
		if propertyID != cfg.GroupPropertyID {
			continue
		}

		options, _ := property["options"].([]interface{})
		for _, option := range options {
			optionMap, isMap := option.(map[string]interface{})
			if !isMap {
				continue
			}
			if optionID, _ := optionMap["id"].(string); optionID == cfg.TargetOptionID {
				return nil
			}
		}

		return model.ErrInvalidRecurrence{
			Field:  model.RecurrenceFieldTargetOption,
			Reason: fmt.Sprintf("option %q is no longer on property %q of board %s", cfg.TargetOptionID, cfg.GroupPropertyID, board.ID),
		}
	}

	return model.ErrInvalidRecurrence{
		Field:  model.RecurrenceFieldGroupProperty,
		Reason: fmt.Sprintf("property %q is no longer on board %s", cfg.GroupPropertyID, board.ID),
	}
}

// logRecurrenceProblem logs at Warn the first time a card fails, and whenever the
// reason changes, and at Debug for every repeat. A scheduler that ticks every
// minute would otherwise write well over a thousand identical lines a day for one
// broken card. The state is in memory and resets on restart, which is fine: the
// first tick after a restart logs once at Warn.
func (a *App) logRecurrenceProblem(cardID string, err error) {
	reason := err.Error()

	a.recurrenceLogMux.Lock()
	previous, seen := a.recurrenceLogState[cardID]
	a.recurrenceLogState[cardID] = reason
	a.recurrenceLogMux.Unlock()

	if seen && previous == reason {
		a.logger.Debug("a recurring card is still failing", mlog.String("cardID", cardID), mlog.Err(err))
		return
	}

	a.logger.Warn("cannot process a recurring card", mlog.String("cardID", cardID), mlog.Err(err))
}

// clearRecurrenceProblem forgets a card that has recovered, so that a later
// failure is reported at Warn rather than swallowed as a repeat.
func (a *App) clearRecurrenceProblem(cardID string) {
	a.recurrenceLogMux.Lock()
	delete(a.recurrenceLogState, cardID)
	a.recurrenceLogMux.Unlock()
}
