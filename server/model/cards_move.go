// Copyright (c) 2015-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

package model

import (
	"errors"
)

var (
	ErrMoveCardsNoCards       = errors.New("at least one card is required")
	ErrMoveCardsEmptyCardID   = errors.New("card ids cannot be empty")
	ErrMoveCardsNoTargetBoard = errors.New("a target board is required")
)

// MoveCardsRequest is the payload to move a set of cards to another board.
// swagger:model
type MoveCardsRequest struct {
	// The ids of the cards to move
	// required: true
	CardIDs []string `json:"cardIDs"`

	// The id of the board the cards are moved to
	// required: true
	ToBoardID string `json:"toBoardID"`
}

// IsValid returns an error if the request is not well formed. Whether the cards and the
// board exist, and whether they can be moved, is decided by the application layer.
func (mcr *MoveCardsRequest) IsValid() error {
	if len(mcr.CardIDs) == 0 {
		return ErrMoveCardsNoCards
	}

	for _, cardID := range mcr.CardIDs {
		if cardID == "" {
			return ErrMoveCardsEmptyCardID
		}
	}

	if mcr.ToBoardID == "" {
		return ErrMoveCardsNoTargetBoard
	}

	return nil
}