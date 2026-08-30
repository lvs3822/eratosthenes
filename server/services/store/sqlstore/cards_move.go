// Copyright (c) 2015-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

package sqlstore

import (
	"encoding/json"
	"fmt"

	sq "github.com/Masterminds/squirrel"
	"github.com/mattermost/focalboard/server/model"
	"github.com/mattermost/focalboard/server/utils"
)

// moveCardsToBoard moves the given cards and their subtrees from fromBoardID to toBoardID.
//
// This is the mechanical half of App.MoveCardsToBoard: every check that needs application
// knowledge (permissions, templates, teams, card ownership) has already been made by the
// caller. Here the cards lose their properties, the attachment blocks are pointed at the
// file copies the caller made, and the moved cards are removed from the cardOrder of the
// source board views.
func (s *SQLStore) moveCardsToBoard(db sq.BaseRunner, cardIDs []string, fromBoardID string, toBoardID string, newFileNames map[string]string, userID string) ([]*model.Block, error) {
	movedBlocks := []*model.Block{}

	for _, cardID := range cardIDs {
		// getSubTree2 is scoped by board, so a card that does not belong to the source
		// board yields no blocks at all.
		blocks, err := s.getSubTree2(db, fromBoardID, cardID, model.QuerySubtreeOptions{})
		if err != nil {
			return nil, err
		}
		if len(blocks) == 0 {
			return nil, model.NewErrNotFound(fmt.Sprintf("block subtree BoardID=%s BlockID=%s", fromBoardID, cardID))
		}

		for _, block := range blocks {
			if block.Fields == nil {
				block.Fields = make(map[string]interface{})
			}

			// a card hangs off its board while its content blocks and comments hang off
			// the card, so only the card itself needs a new parent
			if block.ParentID == fromBoardID {
				block.ParentID = toBoardID
			}

			if block.ID == cardID {
				// the card takes the property schema of the target board with every value
				// unset, the previous values are dropped
				block.Fields["properties"] = map[string]interface{}{}
			}

			applyMovedFileName(block, newFileNames)

			if err := s.moveBlockToBoard(db, block, fromBoardID, toBoardID, userID); err != nil {
				return nil, err
			}

			movedBlocks = append(movedBlocks, block)
		}
	}

	if err := s.removeCardsFromViewOrder(db, fromBoardID, cardIDs, userID); err != nil {
		return nil, err
	}

	return movedBlocks, nil
}

// moveBlockToBoard rewrites the board of a single block and records the new state in the
// block history. insertBlock cannot be reused here because its update path is scoped by
// board_id, so it would match no rows as soon as the board changes.
func (s *SQLStore) moveBlockToBoard(db sq.BaseRunner, block *model.Block, fromBoardID string, toBoardID string, userID string) error {
	block.BoardID = toBoardID
	block.ModifiedBy = userID
	block.UpdateAt = utils.GetMillis()

	fieldsJSON, err := json.Marshal(block.Fields)
	if err != nil {
		return err
	}

	updateQuery := s.getQueryBuilder(db).
		Update(s.tablePrefix+"blocks").
		Where(sq.Eq{"id": block.ID}).
		Where(sq.Eq{"board_id": fromBoardID}).
		Set("board_id", block.BoardID).
		Set("parent_id", block.ParentID).
		Set("fields", fieldsJSON).
		Set("modified_by", block.ModifiedBy).
		Set("update_at", block.UpdateAt)

	result, err := updateQuery.Exec()
	if err != nil {
		return err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return model.NewErrNotFound("block ID=" + block.ID + " on board ID=" + fromBoardID)
	}

	return s.insertMovedBlockHistory(db, block, fieldsJSON)
}

// insertMovedBlockHistory appends the post-move state of a block to the history table, the
// same way insertBlock does after a successful write.
func (s *SQLStore) insertMovedBlockHistory(db sq.BaseRunner, block *model.Block, fieldsJSON []byte) error {
	query := s.getQueryBuilder(db).
		Insert(s.tablePrefix+"blocks_history").
		Columns(
			"channel_id",
			"id",
			"parent_id",
			"created_by",
			"modified_by",
			s.escapeField("schema"),
			"type",
			"title",
			"fields",
			"create_at",
			"update_at",
			"delete_at",
			"board_id",
		).
		Values(
			"",
			block.ID,
			block.ParentID,
			block.CreatedBy,
			block.ModifiedBy,
			block.Schema,
			block.Type,
			block.Title,
			fieldsJSON,
			block.CreateAt,
			block.UpdateAt,
			block.DeleteAt,
			block.BoardID,
		)

	_, err := query.Exec()
	return err
}

// removeCardsFromViewOrder drops the moved cards from the cardOrder of every view of the
// source board, so they do not linger in the manual sort order of a board they have left.
func (s *SQLStore) removeCardsFromViewOrder(db sq.BaseRunner, boardID string, cardIDs []string, userID string) error {
	views, err := s.getBlocksWithType(db, boardID, string(model.TypeView))
	if err != nil {
		return err
	}

	movedCardIDs := make(map[string]bool, len(cardIDs))
	for _, cardID := range cardIDs {
		movedCardIDs[cardID] = true
	}

	for _, view := range views {
		cardOrder, ok := cardOrderFromFields(view.Fields)
		if !ok {
			continue
		}

		newCardOrder := make([]string, 0, len(cardOrder))
		for _, cardID := range cardOrder {
			if !movedCardIDs[cardID] {
				newCardOrder = append(newCardOrder, cardID)
			}
		}

		if len(newCardOrder) == len(cardOrder) {
			continue
		}

		view.Fields["cardOrder"] = newCardOrder

		// the view keeps its board, so the regular write path applies here
		if err := s.insertBlock(db, view, userID); err != nil {
			return err
		}
	}

	return nil
}

// cardOrderFromFields reads the cardOrder field of a view. It is a JSON array of block IDs
// when read back from the database, but code paths that build a view in memory use a
// []string, so both are accepted.
func cardOrderFromFields(fields map[string]interface{}) ([]string, bool) {
	value, ok := fields["cardOrder"]
	if !ok {
		return nil, false
	}

	switch order := value.(type) {
	case []interface{}:
		cardOrder := make([]string, 0, len(order))
		for _, entry := range order {
			id, isString := entry.(string)
			if !isString {
				// a malformed entry is left untouched rather than rewritten
				return nil, false
			}
			cardOrder = append(cardOrder, id)
		}
		return cardOrder, true
	case []string:
		return order, true
	}

	return nil, false
}

// applyMovedFileName points an image or attachment block at the copy of its file that was
// made for the target board. A block whose file could not be copied keeps its original file
// ID, which still resolves through the source path.
func applyMovedFileName(block *model.Block, newFileNames map[string]string) {
	if block.Type != model.TypeImage && block.Type != model.TypeAttachment {
		return
	}

	fileID, ok := block.Fields["fileId"].(string)
	if !ok {
		fileID, ok = block.Fields["attachmentId"].(string)
		if !ok {
			return
		}
	}

	newFileName, ok := newFileNames[fileID]
	if !ok || newFileName == "" {
		return
	}

	block.Fields["fileId"] = newFileName
	delete(block.Fields, "attachmentId")
}