package integrationtests

import (
	"testing"

	"github.com/mattermost/focalboard/server/model"
	"github.com/mattermost/focalboard/server/utils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMoveCards(t *testing.T) {
	t.Run("a non authenticated user should be rejected", func(t *testing.T) {
		th := SetupTestHelper(t).InitBasic()
		defer th.TearDown()

		sourceBoard, cards := th.CreateBoardAndCards(testTeamID, model.BoardTypeOpen, 1)
		targetBoard := th.CreateBoard(testTeamID, model.BoardTypeOpen)

		th.Logout(th.Client)

		request := &model.MoveCardsRequest{
			CardIDs:   []string{cards[0].ID},
			ToBoardID: targetBoard.ID,
		}

		movedCards, resp := th.Client.MoveCards(sourceBoard.ID, request)
		th.CheckUnauthorized(resp)
		require.Nil(t, movedCards)
	})

	t.Run("good", func(t *testing.T) {
		th := SetupTestHelper(t).InitBasic()
		defer th.TearDown()

		sourceBoard, cards := th.CreateBoardAndCards(testTeamID, model.BoardTypeOpen, 2)
		targetBoard := th.CreateBoard(testTeamID, model.BoardTypeOpen)
		card := cards[0]

		request := &model.MoveCardsRequest{
			CardIDs:   []string{card.ID},
			ToBoardID: targetBoard.ID,
		}

		movedCards, resp := th.Client.MoveCards(sourceBoard.ID, request)
		th.CheckOK(resp)
		require.Len(t, movedCards, 1)

		// the card keeps its identity and its content, but loses its properties
		require.Equal(t, card.ID, movedCards[0].ID)
		require.Equal(t, card.Title, movedCards[0].Title)
		require.Equal(t, card.Icon, movedCards[0].Icon)
		require.Equal(t, card.ContentOrder, movedCards[0].ContentOrder)
		require.Equal(t, targetBoard.ID, movedCards[0].BoardID)
		require.NotEmpty(t, card.Properties)
		require.Empty(t, movedCards[0].Properties)

		// and the move is persisted
		fetched, resp := th.Client.GetCard(card.ID)
		th.CheckOK(resp)
		require.Equal(t, targetBoard.ID, fetched.BoardID)
		require.Empty(t, fetched.Properties)

		sourceCards, resp := th.Client.GetCards(sourceBoard.ID, 0, -1)
		th.CheckOK(resp)
		assert.Len(t, sourceCards, 1)
		assert.Equal(t, cards[1].ID, sourceCards[0].ID)

		targetCards, resp := th.Client.GetCards(targetBoard.ID, 0, -1)
		th.CheckOK(resp)
		assert.Len(t, targetCards, 1)
		assert.Equal(t, card.ID, targetCards[0].ID)
	})

	t.Run("no cards is rejected", func(t *testing.T) {
		th := SetupTestHelper(t).InitBasic()
		defer th.TearDown()

		sourceBoard := th.CreateBoard(testTeamID, model.BoardTypeOpen)
		targetBoard := th.CreateBoard(testTeamID, model.BoardTypeOpen)

		request := &model.MoveCardsRequest{
			CardIDs:   []string{},
			ToBoardID: targetBoard.ID,
		}

		movedCards, resp := th.Client.MoveCards(sourceBoard.ID, request)
		th.CheckBadRequest(resp)
		require.Nil(t, movedCards)
	})

	t.Run("too many cards is rejected", func(t *testing.T) {
		th := SetupTestHelper(t).InitBasic()
		defer th.TearDown()

		sourceBoard := th.CreateBoard(testTeamID, model.BoardTypeOpen)
		targetBoard := th.CreateBoard(testTeamID, model.BoardTypeOpen)

		cardIDs := make([]string, 0, 101)
		for i := 0; i < 101; i++ {
			cardIDs = append(cardIDs, utils.NewID(utils.IDTypeCard))
		}

		request := &model.MoveCardsRequest{
			CardIDs:   cardIDs,
			ToBoardID: targetBoard.ID,
		}

		movedCards, resp := th.Client.MoveCards(sourceBoard.ID, request)
		th.CheckBadRequest(resp)
		require.Nil(t, movedCards)
	})

	t.Run("the source board as a target is rejected", func(t *testing.T) {
		th := SetupTestHelper(t).InitBasic()
		defer th.TearDown()

		sourceBoard, cards := th.CreateBoardAndCards(testTeamID, model.BoardTypeOpen, 1)

		request := &model.MoveCardsRequest{
			CardIDs:   []string{cards[0].ID},
			ToBoardID: sourceBoard.ID,
		}

		movedCards, resp := th.Client.MoveCards(sourceBoard.ID, request)
		th.CheckBadRequest(resp)
		require.Nil(t, movedCards)
	})

	t.Run("an unknown target board is rejected", func(t *testing.T) {
		th := SetupTestHelper(t).InitBasic()
		defer th.TearDown()

		sourceBoard, cards := th.CreateBoardAndCards(testTeamID, model.BoardTypeOpen, 1)

		request := &model.MoveCardsRequest{
			CardIDs:   []string{cards[0].ID},
			ToBoardID: utils.NewID(utils.IDTypeBoard),
		}

		movedCards, resp := th.Client.MoveCards(sourceBoard.ID, request)
		th.CheckNotFound(resp)
		require.Nil(t, movedCards)
	})

	t.Run("a card of another board is rejected", func(t *testing.T) {
		th := SetupTestHelper(t).InitBasic()
		defer th.TearDown()

		sourceBoard := th.CreateBoard(testTeamID, model.BoardTypeOpen)
		_, otherCards := th.CreateBoardAndCards(testTeamID, model.BoardTypeOpen, 1)
		targetBoard := th.CreateBoard(testTeamID, model.BoardTypeOpen)

		request := &model.MoveCardsRequest{
			CardIDs:   []string{otherCards[0].ID},
			ToBoardID: targetBoard.ID,
		}

		movedCards, resp := th.Client.MoveCards(sourceBoard.ID, request)
		th.CheckBadRequest(resp)
		require.Nil(t, movedCards)
	})

	t.Run("a template card is rejected", func(t *testing.T) {
		th := SetupTestHelper(t).InitBasic()
		defer th.TearDown()

		sourceBoard := th.CreateBoard(testTeamID, model.BoardTypeOpen)
		targetBoard := th.CreateBoard(testTeamID, model.BoardTypeOpen)

		templateCard, resp := th.Client.CreateCard(sourceBoard.ID, &model.Card{
			Title:      "card template",
			IsTemplate: true,
		}, false)
		th.CheckOK(resp)

		request := &model.MoveCardsRequest{
			CardIDs:   []string{templateCard.ID},
			ToBoardID: targetBoard.ID,
		}

		movedCards, resp := th.Client.MoveCards(sourceBoard.ID, request)
		th.CheckBadRequest(resp)
		require.Nil(t, movedCards)
	})

	t.Run("a target board the user cannot edit is rejected", func(t *testing.T) {
		th := SetupTestHelper(t).InitBasic()
		defer th.TearDown()

		sourceBoard, cards := th.CreateBoardAndCards(testTeamID, model.BoardTypeOpen, 1)

		// a private board of the other user, which user1 is not a member of
		otherUserBoard, resp := th.Client2.CreateBoard(&model.Board{
			TeamID: testTeamID,
			Type:   model.BoardTypePrivate,
		})
		th.CheckOK(resp)

		request := &model.MoveCardsRequest{
			CardIDs:   []string{cards[0].ID},
			ToBoardID: otherUserBoard.ID,
		}

		movedCards, resp := th.Client.MoveCards(sourceBoard.ID, request)
		th.CheckForbidden(resp)
		require.Nil(t, movedCards)
	})
}