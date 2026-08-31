// Copyright (c) 2015-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.
import React, {useState} from 'react'
import {useIntl} from 'react-intl'

import ConfirmationDialogBox from '../components/confirmationDialogBox'
import {sendFlashMessage} from '../components/flashMessages'
import mutator from '../mutator'
import {getMySortedBoards} from '../store/boards'
import {useAppSelector} from '../store/hooks'

type MoveCardsToBoard = {

    // Handed to CardActionsMenu, called with the board the user picked.
    onClickMoveToBoard: (toBoardId: string) => void

    // The caller has to render this outside of the menu. Clicking a menu option
    // fires `menuItemClicked`, MenuWrapper closes, and everything the menu
    // rendered unmounts with it, dialog included.
    moveCardsDialog: JSX.Element | null
}

// useMoveCardsToBoard owns the confirmation dialog for moving cards to another
// board, so that each place showing a card menu only has to render the dialog
// and pass the click handler along.
export const useMoveCardsToBoard = (fromBoardId: string, cardIds: string[], onMoved?: () => void): MoveCardsToBoard => {
    const intl = useIntl()
    const boards = useAppSelector(getMySortedBoards)
    const [toBoardId, setToBoardId] = useState('')

    const moveCards = async () => {

        // Dismiss the dialog before awaiting the move. onMoved usually closes the
        // card dialog, which unmounts this hook, so clearing the state afterwards
        // would be a state update on an unmounted component.
        setToBoardId('')

        try {
            await mutator.moveCardsToBoard(fromBoardId, cardIds, toBoardId)
            onMoved?.()
        } catch (error) {
            
            // The server says why a move was rejected. Show that rather than a
            // generic failure.
            sendFlashMessage({content: (error as Error).message, severity: 'high'})
        }
    }

    if (!toBoardId) {
        return {onClickMoveToBoard: setToBoardId, moveCardsDialog: null}
    }

    const toBoard = boards.find((board) => board.id === toBoardId)
    const boardName = toBoard?.title || intl.formatMessage({id: 'ViewTitle.untitled-board', defaultMessage: 'Untitled board'})

    const moveCardsDialog = (
        <ConfirmationDialogBox
            dialogBox={{
                heading: intl.formatMessage({id: 'MoveCardsToBoard.confirm-heading', defaultMessage: 'Move card to {boardName}?'}, {boardName}),
                subText: intl.formatMessage({id: 'MoveCardsToBoard.confirm-subtext', defaultMessage: 'The card keeps its title, contents and comments. Its property values will be cleared and cannot be recovered.'}),
                confirmButtonText: intl.formatMessage({id: 'MoveCardsToBoard.confirm-button', defaultMessage: 'Move'}),
                destructive: true,
                onConfirm: moveCards,
                onClose: () => setToBoardId(''),
            }}
        />
    )

    return {onClickMoveToBoard: setToBoardId, moveCardsDialog}
}
