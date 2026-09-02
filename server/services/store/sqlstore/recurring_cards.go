package sqlstore

import (
	"database/sql"
	"encoding/json"
	"fmt"

	sq "github.com/Masterminds/squirrel"

	"github.com/mattermost/focalboard/server/model"
	"github.com/mattermost/focalboard/server/utils"

	"github.com/mattermost/mattermost/server/public/shared/mlog"
)

func (s *SQLStore) recurringCardFields() []string {
	return []string{
		"card_id",
		"board_id",
		"active",
		"mode",
		"config",
		"next_run_at",
		"last_run_at",
		"create_at",
		"update_at",
	}
}

// upsertRecurringCard writes the row for a card, inserting it or replacing the
// existing one.
//
// This is a single statement rather than the read-then-write shape used by
// insertBlock, because the transaction the generator would otherwise wrap it in
// is skipped entirely on SQLite. Two writers reach this table: the scheduler
// advancing a card after it fires, and the REST layer saving an edited
// configuration. Only a single statement is atomic against both on every engine.
//
// create_at appears in the inserted columns but deliberately NOT in the update
// set, so that re-upserting an existing row preserves when it was first indexed.
func (s *SQLStore) upsertRecurringCard(db sq.BaseRunner, rc *model.RecurringCard) error {
	if err := rc.CheckValid(); err != nil {
		return fmt.Errorf("error validating recurring card %s: %w", rc.CardID, err)
	}

	// config mirrors blocks.fields, which is JSON on Postgres and TEXT elsewhere.
	// MarshalJSONB must not be used here: it prefixes a 0x01 JSONB version header
	// when binary parameters are enabled, which is correct for the boards table's
	// JSONB columns and would corrupt a JSON one.
	configJSON, err := json.Marshal(rc.Config)
	if err != nil {
		return fmt.Errorf("cannot marshal the configuration of recurring card %s: %w", rc.CardID, err)
	}

	now := utils.GetMillis()
	rc.UpdateAt = now
	if rc.CreateAt == 0 {
		rc.CreateAt = now
	}

	queryValues := map[string]interface{}{
		"card_id":     rc.CardID,
		"board_id":    rc.BoardID,
		"active":      rc.Active,
		"mode":        rc.Mode,
		"config":      configJSON,
		"next_run_at": rc.NextRunAt,
		"last_run_at": rc.LastRunAt,
		"create_at":   rc.CreateAt,
		"update_at":   rc.UpdateAt,
	}

	query := s.getQueryBuilder(db).
		Insert(s.tablePrefix + "recurring_cards").
		SetMap(queryValues)

	if s.dbType == model.MysqlDBType {
		query = query.Suffix(
			"ON DUPLICATE KEY UPDATE board_id = ?, active = ?, mode = ?, config = ?, next_run_at = ?, last_run_at = ?, update_at = ?",
			rc.BoardID, rc.Active, rc.Mode, configJSON, rc.NextRunAt, rc.LastRunAt, rc.UpdateAt)
	} else {
		query = query.Suffix(
			`ON CONFLICT (card_id)
             DO UPDATE SET board_id = EXCLUDED.board_id, active = EXCLUDED.active, mode = EXCLUDED.mode,
			   config = EXCLUDED.config, next_run_at = EXCLUDED.next_run_at,
			   last_run_at = EXCLUDED.last_run_at, update_at = EXCLUDED.update_at`,
		)
	}

	if _, err := query.Exec(); err != nil {
		s.logger.Error(`UpsertRecurringCard error`, mlog.String("cardID", rc.CardID), mlog.Err(err))
		return err
	}

	return nil
}

// updateRecurringCardRun advances only the scheduling columns of a row.
//
// The scheduler uses this instead of round-tripping a whole row through
// upsertRecurringCard, so that firing a card can never overwrite a configuration
// saved concurrently, and saving a configuration can never overwrite scheduling
// state. A nil nextRunAt puts the card back into the not-scheduled state.
func (s *SQLStore) updateRecurringCardRun(db sq.BaseRunner, cardID string, nextRunAt *int64, lastRunAt *int64) error {
	query := s.getQueryBuilder(db).
		Update(s.tablePrefix+"recurring_cards").
		Set("next_run_at", nextRunAt).
		Set("last_run_at", lastRunAt).
		Set("update_at", utils.GetMillis()).
		Where(sq.Eq{"card_id": cardID})

	result, err := query.Exec()
	if err != nil {
		s.logger.Error(`UpdateRecurringCardRun error`, mlog.String("cardID", cardID), mlog.Err(err))
		return err
	}

	// An UPDATE that matches nothing succeeds silently. Reporting it keeps a
	// scheduler that failed to advance a card from firing it again every tick
	// with no signal.
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return model.NewErrNotFound("recurring card ID=" + cardID)
	}

	return nil
}

func (s *SQLStore) deleteRecurringCard(db sq.BaseRunner, cardID string) error {
	query := s.getQueryBuilder(db).
		Delete(s.tablePrefix + "recurring_cards").
		Where(sq.Eq{"card_id": cardID})

	result, err := query.Exec()
	if err != nil {
		s.logger.Error(`DeleteRecurringCard error`, mlog.String("cardID", cardID), mlog.Err(err))
		return err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return model.NewErrNotFound("recurring card ID=" + cardID)
	}

	return nil
}

func (s *SQLStore) getRecurringCard(db sq.BaseRunner, cardID string) (*model.RecurringCard, error) {
	query := s.getQueryBuilder(db).
		Select(s.recurringCardFields()...).
		From(s.tablePrefix + "recurring_cards").
		Where(sq.Eq{"card_id": cardID})

	rows, err := query.Query()
	if err != nil {
		s.logger.Error(`GetRecurringCard ERROR`, mlog.String("cardID", cardID), mlog.Err(err))
		return nil, err
	}
	defer s.CloseRows(rows)

	recurringCards, err := s.recurringCardsFromRows(rows)
	if err != nil {
		return nil, err
	}

	if len(recurringCards) == 0 {
		return nil, model.NewErrNotFound("recurring card ID=" + cardID)
	}

	return recurringCards[0], nil
}

// getDueRecurringCards returns the active rows whose next occurrence has arrived,
// soonest first. Rows with a null next_run_at are not scheduled and are excluded
// by the comparison itself. A limit of zero or less means no limit.
func (s *SQLStore) getDueRecurringCards(db sq.BaseRunner, now int64, limit int) ([]*model.RecurringCard, error) {
	query := s.getQueryBuilder(db).
		Select(s.recurringCardFields()...).
		From(s.tablePrefix + "recurring_cards").
		Where(sq.Eq{"active": true}).
		Where(sq.NotEq{"next_run_at": nil}).
		Where(sq.LtOrEq{"next_run_at": now}).
		OrderBy("next_run_at ASC")

	if limit > 0 {
		query = query.Limit(uint64(limit))
	}

	rows, err := query.Query()
	if err != nil {
		s.logger.Error(`GetDueRecurringCards ERROR`, mlog.Err(err))
		return nil, err
	}
	defer s.CloseRows(rows)

	return s.recurringCardsFromRows(rows)
}

func (s *SQLStore) recurringCardsFromRows(rows *sql.Rows) ([]*model.RecurringCard, error) {
	recurringCards := []*model.RecurringCard{}

	for rows.Next() {
		var recurringCard model.RecurringCard
		var configBytes []byte

		err := rows.Scan(
			&recurringCard.CardID,
			&recurringCard.BoardID,
			&recurringCard.Active,
			&recurringCard.Mode,
			&configBytes,
			&recurringCard.NextRunAt,
			&recurringCard.LastRunAt,
			&recurringCard.CreateAt,
			&recurringCard.UpdateAt,
		)
		if err != nil {
			s.logger.Error("recurringCardsFromRows scan error", mlog.Err(err))
			return nil, err
		}

		// A row whose configuration cannot be read is reported rather than
		// returned empty: a zero-value config would leave the scheduler doing
		// nothing forever with nothing in the log to explain it.
		if err := json.Unmarshal(configBytes, &recurringCard.Config); err != nil {
			s.logger.Error("recurring card config unmarshal error", mlog.String("cardID", recurringCard.CardID), mlog.Err(err))
			return nil, fmt.Errorf("cannot unmarshal the configuration of recurring card %s: %w", recurringCard.CardID, err)
		}

		recurringCards = append(recurringCards, &recurringCard)
	}

	return recurringCards, nil
}
