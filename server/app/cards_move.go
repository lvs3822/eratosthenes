// Copyright (c) 2015-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

package app

import (
	"fmt"

	"github.com/mattermost/focalboard/server/model"

	"github.com/mattermost/mattermost/server/public/shared/mlog"
)

// These are bad requests rather than plain sentinels so that the API layer maps them to a
// 400 through model.IsErrBadRequest. They stay comparable with errors.Is.
var (
	ErrMoveToSameBoard      = model.NewErrBadRequest("the target board is the same as the source board")
	ErrMoveAcrossTeams      = model.NewErrBadRequest("cards cannot be moved to a board of another team")
	ErrMoveTemplateBoard    = model.NewErrBadRequest("cards cannot be moved from or to a template board")
	ErrMoveTemplateCard     = model.NewErrBadRequest("template cards cannot be moved")
	ErrCardNotInSourceBoard = model.NewErrBadRequest("the card does not belong to the source board")
	ErrMoveNotACard         = model.NewErrBadRequest("only cards can be moved between boards")
)

// MoveCardsToBoard moves the given cards, their content blocks and their comments from
// fromBoardID to toBoardID, and returns every block that moved.
//
// Card properties are discarded: after the move fields.properties is an empty map, so the
// cards take the property schema of the target board with every value unset. No attempt is
// made to map properties between the two boards.
//
// Attachment files are copied so they resolve under the target board. The copy happens
// before the transaction because the file backend is not transactional, and the resulting
// file IDs are applied to the blocks inside it. The source files are left where they are on
// purpose; cleaning them up is a separate task.
func (a *App) MoveCardsToBoard(cardIDs []string, fromBoardID string, toBoardID string, userID string) ([]*model.Block, error) {
	if len(cardIDs) == 0 {
		return []*model.Block{}, nil
	}

	if fromBoardID == "" || toBoardID == "" {
		return nil, model.NewErrBadRequest("a source and a target board are required to move cards")
	}

	if fromBoardID == toBoardID {
		return nil, ErrMoveToSameBoard
	}

	sourceBoard, err := a.GetBoard(fromBoardID)
	if err != nil {
		return nil, fmt.Errorf("cannot fetch source board %s for MoveCardsToBoard: %w", fromBoardID, err)
	}

	targetBoard, err := a.GetBoard(toBoardID)
	if err != nil {
		return nil, fmt.Errorf("cannot fetch target board %s for MoveCardsToBoard: %w", toBoardID, err)
	}

	if sourceBoard.IsTemplate || targetBoard.IsTemplate {
		return nil, ErrMoveTemplateBoard
	}

	if sourceBoard.TeamID != targetBoard.TeamID {
		return nil, ErrMoveAcrossTeams
	}

	movableCardIDs, blocksToMove, err := a.validateCardsForMove(cardIDs, fromBoardID)
	if err != nil {
		return nil, err
	}

	newFileNames := map[string]string{}
	if hasFileBlocks(blocksToMove) {
		// CopyCardFiles resolves the destination board from block.BoardID, so the blocks
		// handed to it must already carry the target board. These are our own copies read
		// from the store; the authoritative write happens inside the transaction below.
		for _, block := range blocksToMove {
			block.BoardID = toBoardID
		}

		newFileNames, err = a.CopyCardFiles(fromBoardID, blocksToMove, false)
		if err != nil {
			return nil, fmt.Errorf("cannot copy the files of the moved cards: %w", err)
		}
	}

	movedBlocks, err := a.store.MoveCardsToBoard(movableCardIDs, fromBoardID, toBoardID, newFileNames, userID)
	if err != nil {
		return nil, err
	}

	a.metrics.IncrementBlocksPatched(len(movedBlocks))

	// This can be synchronous because this action is not common.
	for _, block := range movedBlocks {
		// the block is gone for anybody watching the source board
		a.wsAdapter.BroadcastBlockDelete(sourceBoard.TeamID, block.ID, fromBoardID)
		// and it is new for anybody watching the target board
		a.wsAdapter.BroadcastBlockChange(targetBoard.TeamID, block)
	}

	a.broadcastBoardViews(sourceBoard)

	// TODO: webhooks (a.webhook.NotifyUpdate) and subscription notifications
	// (a.notifyBlockChanged) are deliberately not fired for a move yet.

	return movedBlocks, nil
}

// validateCardsForMove checks that every card is allowed to leave fromBoardID and returns
// the deduplicated card IDs together with every block that will move: the cards, their
// content blocks and their comments.
func (a *App) validateCardsForMove(cardIDs []string, fromBoardID string) ([]string, []*model.Block, error) {
	movableCardIDs := make([]string, 0, len(cardIDs))
	blocksToMove := []*model.Block{}
	seen := make(map[string]bool, len(cardIDs))

	for _, cardID := range cardIDs {
		if seen[cardID] {
			continue
		}
		seen[cardID] = true

		card, err := a.store.GetBlock(cardID)
		if err != nil {
			return nil, nil, fmt.Errorf("cannot fetch card %s for MoveCardsToBoard: %w", cardID, err)
		}

		// a deleted block is removed from the blocks table, so the store already reports it
		// as not found. This guards the case where it is only flagged as deleted.
		if card.DeleteAt != 0 {
			return nil, nil, model.NewErrNotFound("card ID=" + cardID)
		}

		if card.Type != model.TypeCard {
			return nil, nil, fmt.Errorf("block %s: %w", cardID, ErrMoveNotACard)
		}

		if card.BoardID != fromBoardID {
			return nil, nil, fmt.Errorf("card %s: %w", cardID, ErrCardNotInSourceBoard)
		}

		if isTemplate, ok := card.Fields["isTemplate"].(bool); ok && isTemplate {
			return nil, nil, fmt.Errorf("card %s: %w", cardID, ErrMoveTemplateCard)
		}

		// GetSubTree2 returns the card plus its direct children, which is where the content
		// blocks and the comments of a card live.
		subTree, err := a.store.GetSubTree2(fromBoardID, cardID, model.QuerySubtreeOptions{})
		if err != nil {
			return nil, nil, fmt.Errorf("cannot fetch the subtree of card %s for MoveCardsToBoard: %w", cardID, err)
		}

		movableCardIDs = append(movableCardIDs, cardID)
		blocksToMove = append(blocksToMove, subTree...)
	}

	return movableCardIDs, blocksToMove, nil
}

// broadcastBoardViews re-reads the views of a board and broadcasts them, so that clients
// watching it pick up the cardOrder entries removed by the move instead of writing their
// stale copy back on the next manual reorder.
func (a *App) broadcastBoardViews(board *model.Board) {
	views, err := a.store.GetBlocksWithType(board.ID, string(model.TypeView))
	if err != nil {
		a.logger.Error("MoveCardsToBoard: cannot fetch the source board views to broadcast",
			mlog.String("boardID", board.ID),
			mlog.Err(err),
		)
		return
	}

	for _, view := range views {
		a.wsAdapter.BroadcastBlockChange(board.TeamID, view)
	}
}

func hasFileBlocks(blocks []*model.Block) bool {
	for _, block := range blocks {
		if block.Type == model.TypeImage || block.Type == model.TypeAttachment {
			return true
		}
	}
	return false
}