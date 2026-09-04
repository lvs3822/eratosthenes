// Copyright (c) 2015-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

package app

import (
	"errors"
	"fmt"
	"reflect"
	"time"

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

	// The row's copy of the configuration can fall behind the card's if only one of
	// the two writes in SetCardRecurrence landed, which is possible on SQLite where
	// the transaction is skipped. The card is authoritative, so bring the row up to
	// date and let the next tick act on the corrected row rather than materialising
	// from a stale schedule.
	if !reflect.DeepEqual(row.Config, cfg) {
		return a.refreshRecurringCard(row, cfg)
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

// refreshRecurringCard rewrites a row whose stored configuration has fallen behind
// the card's. It keeps the scheduling columns, since only the configuration drifted.
func (a *App) refreshRecurringCard(row *model.RecurringCard, cfg *model.RecurrenceConfig) error {
	refreshed := *row
	refreshed.Config = cfg
	refreshed.Mode = cfg.Mode
	refreshed.Active = true

	a.logger.Warn("the stored configuration of a recurring card had fallen behind the card, refreshing it",
		mlog.String("cardID", row.CardID))

	return a.store.UpsertRecurringCard(&refreshed)
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
			// The occurrence carries no rule, so that it cannot recur on its own,
			// but it does carry a reference back to the card that does. The
			// done-column trigger needs it: in "afterDone" mode the source card
			// never leaves the done column itself, so without this the chain would
			// stop after a single occurrence.
			model.CardFieldRecurrenceSourceID: card.ID,
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

// SetCardRecurrence creates or replaces the recurrence configuration of a card,
// writing both the card's fields.recurrence and its row in one store call.
//
// ATOMICITY IS DIALECT DEPENDENT, and the call below cannot change that. The store
// method is annotated @withTransaction, so on MySQL and Postgres the two writes
// commit together or not at all. On SQLite the generated wrapper skips the
// transaction entirely (see public_methods.go), so the two writes are independent
// and safety comes from their ORDER instead: the row is written first, because a
// row whose card is not recurring is cleaned up by the scheduler on its next tick,
// whereas a card claiming to recur with no row would be invisible to it.
func (a *App) SetCardRecurrence(cardID string, cfg *model.RecurrenceConfig, userID string) (*model.RecurringCard, error) {
	card, err := a.recurrenceCard(cardID)
	if err != nil {
		return nil, err
	}

	row, _, err := a.prepareRecurrence(card, cfg)
	if err != nil {
		return nil, err
	}

	fields := map[string]interface{}{}
	if fieldsErr := model.SetCardRecurrenceFields(fields, model.CardTypeRecurring, row.Config); fieldsErr != nil {
		return nil, fieldsErr
	}

	patch := &model.BlockPatch{UpdatedFields: fields}

	if err := a.store.SetCardRecurrence(cardID, patch, row, userID); err != nil {
		return nil, err
	}

	a.broadcastCardChange(card)

	return row, nil
}

// DeleteCardRecurrence stops a card recurring and removes its row.
//
// The configuration itself is kept on the card. Turning the switch off is exactly
// the action that must not discard what was set up, so that turning it back on
// restores the settings rather than presenting an empty form. A card of type
// "normal" is invisible to the scheduler regardless, and its row is gone.
func (a *App) DeleteCardRecurrence(cardID string, userID string) error {
	card, err := a.recurrenceCard(cardID)
	if err != nil {
		return err
	}

	patch := &model.BlockPatch{DeletedFields: []string{model.CardFieldCardType}}

	_, existing, fieldsErr := model.CardRecurrenceFromFields(card.Fields)
	if fieldsErr == nil && existing != nil {
		// Enabled is cleared even though the rest is kept: a normal card carrying an
		// enabled recurrence is a combination the validator rejects.
		disabled := *existing
		disabled.Enabled = false

		fields := map[string]interface{}{}
		if err := model.SetCardRecurrenceFields(fields, model.CardTypeNormal, &disabled); err != nil {
			return err
		}
		patch.UpdatedFields = fields
	}

	// A nil row means "remove it"; see SetCardRecurrence for why this is one call.
	if err := a.store.SetCardRecurrence(cardID, patch, nil, userID); err != nil {
		return err
	}

	a.broadcastCardChange(card)

	return nil
}

// PreviewCardRecurrence reports what saving a configuration would do, without
// saving it. It runs the same preparation as SetCardRecurrence so that what the
// preview shows and what a save enforces cannot drift apart.
func (a *App) PreviewCardRecurrence(cardID string, cfg *model.RecurrenceConfig) (*model.RecurrencePreview, error) {
	card, err := a.recurrenceCard(cardID)
	if err != nil {
		return nil, err
	}

	// The validation error is the answer here rather than a failure: an invalid
	// configuration is precisely what the caller is asking about.
	_, preview, _ := a.prepareRecurrence(card, cfg)

	return preview, nil
}

// prepareRecurrence turns a submitted configuration into the row that would be
// stored and the report that describes it. It is the single place both the write
// and the preview go through.
//
// The returned error is the write gate: nil means the configuration may be saved.
// The preview is returned whether or not the gate passes.
func (a *App) prepareRecurrence(card *model.Block, cfg *model.RecurrenceConfig) (*model.RecurringCard, *model.RecurrencePreview, error) {
	if cfg == nil {
		return nil, nil, model.ErrNilRecurrence
	}

	prepared := *cfg
	prepared.StartAt = a.recurrenceStartAt(card, cfg)

	problems := model.CardRecurrenceProblems(model.CardTypeRecurring, &prepared)

	preview := &model.RecurrencePreview{
		Valid:    len(problems) == 0,
		Problems: problems,
	}

	if preview.Valid {
		nextRunAt, err := nextRunForNewConfig(&prepared)
		if err != nil {
			return nil, preview, err
		}
		preview.NextRunAt = nextRunAt
	}

	if err := model.CheckCardRecurrenceWritable(model.CardTypeRecurring, &prepared); err != nil {
		return nil, preview, err
	}

	row := &model.RecurringCard{
		CardID:    card.ID,
		BoardID:   card.BoardID,
		Active:    model.IsRecurrenceActive(model.CardTypeRecurring, &prepared),
		Mode:      prepared.Mode,
		Config:    &prepared,
		NextRunAt: preview.NextRunAt,
		LastRunAt: a.recurrenceLastRunAt(card.ID),
	}

	return row, preview, nil
}

// recurrenceStartAt decides the phase anchor of a configuration being saved.
//
// The anchor belongs to the server, not to whatever the client submitted. It is set
// once, when a card is first made recurring, and carried forward through every
// later edit and through a pause, so that changing an interval or adding a weekday
// does not shift the cycle. A settings form that round-trips the whole object would
// otherwise reschedule the recurrence on every save, with nothing looking wrong.
func (a *App) recurrenceStartAt(card *model.Block, cfg *model.RecurrenceConfig) int64 {
	if _, existing, err := model.CardRecurrenceFromFields(card.Fields); err == nil &&
		existing != nil && existing.StartAt > 0 {
		return existing.StartAt
	}

	// Anchoring to now rather than to the card's creation time: a card may have
	// existed for months before being made recurring, and "every other week starting
	// now" is what someone enabling it means.
	return utils.GetMillis()
}

// recurrenceLastRunAt carries the record of when an occurrence was last produced
// across a configuration change. Editing a rule does not produce an occurrence, so
// the column must not be reset by doing it.
func (a *App) recurrenceLastRunAt(cardID string) *int64 {
	row, err := a.store.GetRecurringCard(cardID)
	if err != nil {
		return nil
	}

	return row.LastRunAt
}

// nextRunForNewConfig computes the first occurrence of a configuration being saved.
func nextRunForNewConfig(cfg *model.RecurrenceConfig) (*int64, error) {
	// An "afterDone" recurrence is not scheduled until the card reaches the column
	// that counts as completion, so it starts with no next run at all. The trigger
	// that watches the done column sets it.
	if cfg.Mode == model.RecurrenceModeAfterDone {
		return nil, nil
	}

	nextRunAt, err := model.NextRunAt(cfg, time.Now())
	if err != nil {
		return nil, err
	}

	return &nextRunAt, nil
}

func (a *App) recurrenceCard(cardID string) (*model.Block, error) {
	card, err := a.store.GetBlock(cardID)
	if err != nil {
		return nil, err
	}

	if card.Type != model.TypeCard {
		return nil, model.ErrNotCardBlock
	}

	return card, nil
}

// broadcastCardChange tells open clients that a card changed. Setting a recurrence
// bypasses PatchBlockAndNotify, which would otherwise do this, because the card and
// its row have to be written together.
func (a *App) broadcastCardChange(card *model.Block) {
	board, err := a.store.GetBoard(card.BoardID)
	if err != nil {
		a.logger.Error("cannot load the board to broadcast a recurrence change",
			mlog.String("cardID", card.ID), mlog.Err(err))
		return
	}

	updated, err := a.store.GetBlock(card.ID)
	if err != nil {
		a.logger.Error("cannot reload a card to broadcast a recurrence change",
			mlog.String("cardID", card.ID), mlog.Err(err))
		return
	}

	a.blockChangeNotifier.Enqueue(func() error {
		a.wsAdapter.BroadcastBlockChange(board.TeamID, updated)
		return nil
	})
}
