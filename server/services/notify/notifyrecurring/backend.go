// Copyright (c) 2015-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

// Package notifyrecurring reacts to card changes on behalf of the recurring cards
// feature. It sets the next run of an "afterDone" recurrence when a card reaches
// the column that counts as completion, clears it again if the card is moved back
// out, and reactivates a recurrence whose card has been undeleted.
//
// It never creates or deletes rows in the recurring_cards table; that belongs to
// the layer that accepts the user's configuration.
package notifyrecurring

import (
	"fmt"
	"sync"
	"time"

	"github.com/mattermost/focalboard/server/model"
	"github.com/mattermost/focalboard/server/services/notify"
	"github.com/mattermost/focalboard/server/services/store"

	"github.com/mattermost/mattermost/server/public/shared/mlog"
)

const backendName = "notifyRecurringCards"

type Backend struct {
	store  store.Store
	logger mlog.LoggerIFace

	// problemState remembers the last reason each card was reported for, so a card
	// that is edited repeatedly while its configuration is broken produces one
	// warning rather than one per edit. Same discipline as the App uses for the
	// scheduler; a separate map because a notification backend has no App.
	problemMux   sync.Mutex
	problemState map[string]string
}

func New(store store.Store, logger mlog.LoggerIFace) *Backend {
	return &Backend{
		store:        store,
		logger:       logger,
		problemState: make(map[string]string),
	}
}

func (b *Backend) Start() error { return nil }

func (b *Backend) ShutDown() error { return nil }

func (b *Backend) Name() string { return backendName }

// BlockChanged is called for every block the server changes, so the rejections are
// ordered cheapest first and nothing reaches the database until a change has been
// established as one this backend cares about.
func (b *Backend) BlockChanged(evt notify.BlockChangeEvent) error {
	// 1. Two string comparisons reject deletes, and everything that is not a card:
	//    every text block, image, view and comment on the instance.
	if evt.Action != notify.Update && evt.Action != notify.Add {
		return nil
	}

	card := evt.BlockChanged
	if card == nil || card.Type != model.TypeCard {
		return nil
	}

	if evt.Action == notify.Add {
		// An undelete arrives as an Add with no previous block, not as an Update:
		// UndeleteBlock calls notifyBlockChanged(notify.Add, block, nil, ...). The
		// reachable signal is therefore an Add for a card that already has a row,
		// which a genuinely new card does not, and which duplication cannot produce
		// because DuplicateBlock emits no notification at all.
		return b.restoreUndeletedCard(card)
	}

	if evt.BlockOld == nil {
		return nil
	}

	return b.handleCardUpdate(evt.BlockOld, card)
}

func (b *Backend) handleCardUpdate(oldCard, card *model.Block) error {
	// 2. One map lookup rejects every ordinary card, which is the common case by a
	//    wide margin. Only cards that are themselves recurring, or that are an
	//    occurrence of one, go any further.
	sourceCardID, ok := recurrenceSourceID(card)
	if !ok {
		return nil
	}

	// 3. Reading the rule costs a JSON round trip, and resolving a back reference
	//    costs a block read, so both sit behind the check above.
	cfg, err := b.resolveRecurrence(card, sourceCardID)
	if err != nil {
		b.reportProblem(card.ID, err)
		return nil
	}
	if cfg == nil {
		return nil
	}

	// 4. Only "afterDone" recurrences are driven by the done column. A schedule
	//    recurrence is driven by its rule and must be left alone.
	if cfg.Mode != model.RecurrenceModeAfterDone || !cfg.Enabled {
		return nil
	}

	// 5. Firing on the transition, not on the state, is what stops every later edit
	//    to a card sitting in the done column from rescheduling it.
	wasDone := propertyValue(oldCard, cfg.GroupPropertyID) == cfg.DoneOptionID
	isDone := propertyValue(card, cfg.GroupPropertyID) == cfg.DoneOptionID

	switch {
	case isDone && !wasDone:
		return b.scheduleAfterDone(sourceCardID, cfg, card.ID)
	case wasDone && !isDone:
		// Moved back out before the delay elapsed: the completion is undone, so the
		// occurrence it would have produced is cancelled too.
		return b.cancelAfterDone(sourceCardID, card.ID)
	default:
		return nil
	}
}

// recurrenceSourceID returns the id of the card whose rule governs this one.
//
// A recurring card governs itself. An occurrence created from one carries no rule,
// so that it cannot recur on its own, but does carry a reference back to the card
// that does; without it the chain would stop after a single iteration, because the
// source card never leaves the done column itself.
func recurrenceSourceID(card *model.Block) (string, bool) {
	if cardType, isString := card.Fields[model.CardFieldCardType].(string); isString &&
		model.CardType(cardType) == model.CardTypeRecurring {
		return card.ID, true
	}

	if sourceID, isString := card.Fields[model.CardFieldRecurrenceSourceID].(string); isString && sourceID != "" {
		return sourceID, true
	}

	return "", false
}

// resolveRecurrence returns the rule governing a card, reading it from the card
// itself when the card is the source, and from the referenced card otherwise.
// A nil configuration and a nil error mean there is nothing to do.
func (b *Backend) resolveRecurrence(card *model.Block, sourceCardID string) (*model.RecurrenceConfig, error) {
	if sourceCardID == card.ID {
		_, cfg, err := model.CardRecurrenceFromFields(card.Fields)
		return cfg, err
	}

	sourceCard, err := b.store.GetBlock(sourceCardID)
	if model.IsErrNotFound(err) {
		return nil, fmt.Errorf("the source card %s of this occurrence no longer exists: %w", sourceCardID, err)
	}
	if err != nil {
		return nil, err
	}

	cardType, cfg, err := model.CardRecurrenceFromFields(sourceCard.Fields)
	if err != nil {
		return nil, err
	}

	if cardType != model.CardTypeRecurring || cfg == nil {
		return nil, fmt.Errorf("the source card %s of this occurrence is no longer recurring", sourceCardID) //nolint:goerr113
	}

	return cfg, nil
}

func (b *Backend) scheduleAfterDone(sourceCardID string, cfg *model.RecurrenceConfig, completedCardID string) error {
	// NextRunAt owns what "delayDays later" means, including landing at the
	// configured time of day in the configured zone rather than at whatever time
	// the card happened to be completed.
	nextRunAt, err := model.NextRunAt(cfg, time.Now())
	if err != nil {
		b.reportProblem(sourceCardID, err)
		return nil
	}

	b.logger.Debug("Scheduling the next occurrence of a recurring card",
		mlog.String("sourceCardID", sourceCardID),
		mlog.String("completedCardID", completedCardID),
	)

	return b.setNextRun(sourceCardID, &nextRunAt)
}

func (b *Backend) cancelAfterDone(sourceCardID string, completedCardID string) error {
	b.logger.Debug("Cancelling the next occurrence of a recurring card that left the done column",
		mlog.String("sourceCardID", sourceCardID),
		mlog.String("completedCardID", completedCardID),
	)

	return b.setNextRun(sourceCardID, nil)
}

// setNextRun writes the scheduling column of the source card's row, leaving
// last_run_at where it is. Completing a card is not producing an occurrence, so
// the record of when one was last produced must not move.
func (b *Backend) setNextRun(sourceCardID string, nextRunAt *int64) error {
	row, err := b.store.GetRecurringCard(sourceCardID)
	if model.IsErrNotFound(err) {
		// Everything above has already established this is an enabled afterDone
		// recurrence making a real transition, so a missing row is drift between the
		// card and the index rather than an ordinary absence. Reported rather than
		// repaired: creating rows belongs to the layer that accepts the config.
		b.reportProblem(sourceCardID, err)
		return nil
	}
	if err != nil {
		return err
	}

	return b.store.UpdateRecurringCardRun(sourceCardID, nextRunAt, row.LastRunAt)
}

// restoreUndeletedCard reactivates the row of a recurring card that has come back.
//
// A soft delete leaves the row in place but inactive, so that undeleting the card
// restores the recurrence rather than silently losing it. This only touches the
// active flag: an undelete is not a completion and must not reschedule anything.
func (b *Backend) restoreUndeletedCard(card *model.Block) error {
	cardType, isString := card.Fields[model.CardFieldCardType].(string)
	if !isString || model.CardType(cardType) != model.CardTypeRecurring {
		return nil
	}

	_, cfg, err := model.CardRecurrenceFromFields(card.Fields)
	if err != nil || cfg == nil {
		return nil
	}

	row, err := b.store.GetRecurringCard(card.ID)
	if model.IsErrNotFound(err) {
		// A card being created rather than undeleted. Its row does not exist yet.
		return nil
	}
	if err != nil {
		return err
	}

	if row.Active {
		return nil
	}

	restored := *row
	restored.Active = model.IsRecurrenceActive(model.CardType(cardType), cfg)
	restored.Config = cfg
	restored.Mode = cfg.Mode

	b.logger.Debug("Reactivating the recurrence of an undeleted card", mlog.String("cardID", card.ID))

	return b.store.UpsertRecurringCard(&restored)
}

// propertyValue returns a card's value for a property, or the empty string when it
// has none. Group-by properties are single valued, so anything else is treated as
// absent rather than guessed at.
func propertyValue(card *model.Block, propertyID string) string {
	raw, ok := card.Fields["properties"]
	if !ok {
		return ""
	}

	properties, isMap := raw.(map[string]interface{})
	if !isMap {
		return ""
	}

	value, isString := properties[propertyID].(string)
	if !isString {
		return ""
	}

	return value
}

// reportProblem logs at Warn the first time a card fails and whenever the reason
// changes, and at Debug for repeats, so that a card edited repeatedly while its
// configuration is broken does not flood the log.
func (b *Backend) reportProblem(cardID string, err error) {
	reason := err.Error()

	b.problemMux.Lock()
	previous, seen := b.problemState[cardID]
	b.problemState[cardID] = reason
	b.problemMux.Unlock()

	if seen && previous == reason {
		b.logger.Debug("A recurring card is still failing", mlog.String("cardID", cardID), mlog.Err(err))
		return
	}

	b.logger.Warn("Cannot apply a recurring card change", mlog.String("cardID", cardID), mlog.Err(err))
}
