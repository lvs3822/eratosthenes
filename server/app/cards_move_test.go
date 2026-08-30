// Copyright (c) 2015-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

package app

import (
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/require"

	"github.com/mattermost/focalboard/server/model"
)

const (
	testTargetBoardID = "test-target-board-id"
	testMoveTeamID    = "test-team-id"
	testMoveUserID    = "user-id-1"
)

func newTestCard(cardID string, boardID string) *model.Block {
	return &model.Block{
		ID:       cardID,
		ParentID: boardID,
		BoardID:  boardID,
		Type:     model.TypeCard,
		Fields: map[string]interface{}{
			"properties": map[string]interface{}{"property-id": "option-id"},
		},
	}
}

func TestMoveCardsToBoard(t *testing.T) {
	th, tearDown := SetupTestHelper(t)
	defer tearDown()

	sourceBoard := &model.Board{ID: testBoardID, TeamID: testMoveTeamID}
	targetBoard := &model.Board{ID: testTargetBoardID, TeamID: testMoveTeamID}

	th.Store.EXPECT().GetBoard(testBoardID).Return(sourceBoard, nil).AnyTimes()
	th.Store.EXPECT().GetBoard(testTargetBoardID).Return(targetBoard, nil).AnyTimes()
	th.Store.EXPECT().GetMembersForBoard(gomock.Any()).Return([]*model.BoardMember{}, nil).AnyTimes()

	t.Run("success scenario", func(t *testing.T) {
		card := newTestCard("card-id", testBoardID)
		movedCard := newTestCard("card-id", testTargetBoardID)

		th.Store.EXPECT().GetBlock("card-id").Return(card, nil)
		th.Store.EXPECT().GetSubTree2(testBoardID, "card-id", model.QuerySubtreeOptions{}).
			Return([]*model.Block{card}, nil)
		th.Store.EXPECT().MoveCardsToBoard(
			[]string{"card-id"},
			testBoardID,
			testTargetBoardID,
			map[string]string{},
			testMoveUserID,
		).Return([]*model.Block{movedCard}, nil)
		th.Store.EXPECT().GetBlocksWithType(testBoardID, "view").Return([]*model.Block{}, nil)

		movedBlocks, err := th.App.MoveCardsToBoard([]string{"card-id"}, testBoardID, testTargetBoardID, testMoveUserID)
		require.NoError(t, err)
		require.Len(t, movedBlocks, 1)
		require.Equal(t, testTargetBoardID, movedBlocks[0].BoardID)
	})

	t.Run("duplicated card ids are moved once", func(t *testing.T) {
		card := newTestCard("card-id-2", testBoardID)
		movedCard := newTestCard("card-id-2", testTargetBoardID)

		th.Store.EXPECT().GetBlock("card-id-2").Return(card, nil)
		th.Store.EXPECT().GetSubTree2(testBoardID, "card-id-2", model.QuerySubtreeOptions{}).
			Return([]*model.Block{card}, nil)
		th.Store.EXPECT().MoveCardsToBoard(
			[]string{"card-id-2"},
			testBoardID,
			testTargetBoardID,
			map[string]string{},
			testMoveUserID,
		).Return([]*model.Block{movedCard}, nil)
		th.Store.EXPECT().GetBlocksWithType(testBoardID, "view").Return([]*model.Block{}, nil)

		movedBlocks, err := th.App.MoveCardsToBoard(
			[]string{"card-id-2", "card-id-2"},
			testBoardID, testTargetBoardID, testMoveUserID,
		)
		require.NoError(t, err)
		require.Len(t, movedBlocks, 1)
	})

	t.Run("no card ids is a no-op", func(t *testing.T) {
		movedBlocks, err := th.App.MoveCardsToBoard([]string{}, testBoardID, testTargetBoardID, testMoveUserID)
		require.NoError(t, err)
		require.Empty(t, movedBlocks)
	})

	t.Run("store error is propagated", func(t *testing.T) {
		card := newTestCard("card-id-3", testBoardID)

		th.Store.EXPECT().GetBlock("card-id-3").Return(card, nil)
		th.Store.EXPECT().GetSubTree2(testBoardID, "card-id-3", model.QuerySubtreeOptions{}).
			Return([]*model.Block{card}, nil)
		th.Store.EXPECT().MoveCardsToBoard(
			[]string{"card-id-3"},
			testBoardID,
			testTargetBoardID,
			map[string]string{},
			testMoveUserID,
		).Return(nil, blockError{"error"})

		movedBlocks, err := th.App.MoveCardsToBoard([]string{"card-id-3"}, testBoardID, testTargetBoardID, testMoveUserID)
		require.Error(t, err, "error")
		require.Nil(t, movedBlocks)
	})
}

func TestMoveCardsToBoardRejections(t *testing.T) {
	th, tearDown := SetupTestHelper(t)
	defer tearDown()

	const (
		templateBoardID  = "template-board-id"
		otherTeamBoardID = "other-team-board-id"
		otherBoardID     = "other-board-id"
	)

	sourceBoard := &model.Board{ID: testBoardID, TeamID: testMoveTeamID}
	targetBoard := &model.Board{ID: testTargetBoardID, TeamID: testMoveTeamID}
	templateBoard := &model.Board{ID: templateBoardID, TeamID: testMoveTeamID, IsTemplate: true}
	otherTeamBoard := &model.Board{ID: otherTeamBoardID, TeamID: "another-team-id"}

	th.Store.EXPECT().GetBoard(testBoardID).Return(sourceBoard, nil).AnyTimes()
	th.Store.EXPECT().GetBoard(testTargetBoardID).Return(targetBoard, nil).AnyTimes()
	th.Store.EXPECT().GetBoard(templateBoardID).Return(templateBoard, nil).AnyTimes()
	th.Store.EXPECT().GetBoard(otherTeamBoardID).Return(otherTeamBoard, nil).AnyTimes()

	t.Run("the same board is rejected", func(t *testing.T) {
		_, err := th.App.MoveCardsToBoard([]string{"card-id"}, testBoardID, testBoardID, testMoveUserID)
		require.ErrorIs(t, err, ErrMoveToSameBoard)
		require.True(t, model.IsErrBadRequest(err))
	})

	t.Run("a board of another team is rejected", func(t *testing.T) {
		_, err := th.App.MoveCardsToBoard([]string{"card-id"}, testBoardID, otherTeamBoardID, testMoveUserID)
		require.ErrorIs(t, err, ErrMoveAcrossTeams)
		require.True(t, model.IsErrBadRequest(err))
	})

	t.Run("a template board is rejected", func(t *testing.T) {
		_, err := th.App.MoveCardsToBoard([]string{"card-id"}, testBoardID, templateBoardID, testMoveUserID)
		require.ErrorIs(t, err, ErrMoveTemplateBoard)
	})

	t.Run("a template card is rejected", func(t *testing.T) {
		card := newTestCard("template-card-id", testBoardID)
		card.Fields["isTemplate"] = true
		th.Store.EXPECT().GetBlock("template-card-id").Return(card, nil)

		_, err := th.App.MoveCardsToBoard([]string{"template-card-id"}, testBoardID, testTargetBoardID, testMoveUserID)
		require.ErrorIs(t, err, ErrMoveTemplateCard)
		require.True(t, model.IsErrBadRequest(err))
	})

	t.Run("a card of another board is rejected", func(t *testing.T) {
		card := newTestCard("foreign-card-id", otherBoardID)
		th.Store.EXPECT().GetBlock("foreign-card-id").Return(card, nil)

		_, err := th.App.MoveCardsToBoard([]string{"foreign-card-id"}, testBoardID, testTargetBoardID, testMoveUserID)
		require.ErrorIs(t, err, ErrCardNotInSourceBoard)
		require.True(t, model.IsErrBadRequest(err))
	})

	t.Run("a block that is not a card is rejected", func(t *testing.T) {
		block := &model.Block{ID: "text-block-id", BoardID: testBoardID, Type: model.TypeText}
		th.Store.EXPECT().GetBlock("text-block-id").Return(block, nil)

		_, err := th.App.MoveCardsToBoard([]string{"text-block-id"}, testBoardID, testTargetBoardID, testMoveUserID)
		require.ErrorIs(t, err, ErrMoveNotACard)
		require.True(t, model.IsErrBadRequest(err))
	})

	t.Run("a deleted card is reported as not found", func(t *testing.T) {
		th.Store.EXPECT().GetBlock("deleted-card-id").
			Return(nil, model.NewErrNotFound("block ID=deleted-card-id"))

		_, err := th.App.MoveCardsToBoard([]string{"deleted-card-id"}, testBoardID, testTargetBoardID, testMoveUserID)
		require.True(t, model.IsErrNotFound(err))
	})
}