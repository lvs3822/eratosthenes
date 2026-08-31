// Copyright (c) 2015-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

import React, {ReactNode, useMemo} from 'react'
import {useIntl} from 'react-intl'

import DeleteIcon from '../../widgets/icons/delete'
import Menu from '../../widgets/menu'
import BoardPermissionGate from '../permissions/boardPermissionGate'
import DuplicateIcon from '../../widgets/icons/duplicate'
import LinkIcon from '../../widgets/icons/Link'
import CompassIcon from '../../widgets/icons/compassIcon'
import {Utils} from '../../utils'
import {Permission} from '../../constants'
import {sendFlashMessage} from '../flashMessages'
import {IUser} from '../../user'
import {getMe} from '../../store/users'
import {getMySortedBoards} from '../../store/boards'
import {useAppSelector} from '../../store/hooks'
import TelemetryClient, {TelemetryActions, TelemetryCategory} from '../../telemetry/telemetryClient'

type Props = {
    cardId: string
    boardId: string
    onClickDelete: () => void
    onClickDuplicate?: () => void
    onClickMoveToBoard?: (toBoardId: string) => void
    children?: ReactNode
}

export const CardActionsMenu = (props: Props): JSX.Element => {
    const {cardId} = props

    const me = useAppSelector<IUser|null>(getMe)
    const boards = useAppSelector(getMySortedBoards)
    const intl = useIntl()

    // getMySortedBoards only holds the current team's boards and keeps templates
    // in a separate slice, so the current board is all that is left to exclude.
    const moveToBoards = useMemo(() => boards.filter((board) => board.id !== props.boardId), [boards, props.boardId])

    const handleDeleteCard = () => {
        TelemetryClient.trackEvent(TelemetryCategory, TelemetryActions.DeleteCard, {board: props.boardId, card: props.cardId})
        props.onClickDelete()
    }

    const handleDuplicateCard = () => {
        if (props.onClickDuplicate) {
            TelemetryClient.trackEvent(TelemetryCategory, TelemetryActions.DuplicateCard, {board: props.boardId, card: props.cardId})
            props.onClickDuplicate()
        }
    }

    return (
        <Menu position='left'>
            <BoardPermissionGate permissions={[Permission.ManageBoardCards]}>
                <Menu.Text
                    icon={<DeleteIcon/>}
                    id='delete'
                    name={intl.formatMessage({id: 'CardActionsMenu.delete', defaultMessage: 'Delete'})}
                    onClick={handleDeleteCard}
                />
                {props.onClickDuplicate &&
                <Menu.Text
                    icon={<DuplicateIcon/>}
                    id='duplicate'
                    name={intl.formatMessage({id: 'CardActionsMenu.duplicate', defaultMessage: 'Duplicate'})}
                    onClick={handleDuplicateCard}
                />}
            </BoardPermissionGate>
            {props.onClickMoveToBoard && moveToBoards.length > 0 &&
            <BoardPermissionGate permissions={[Permission.ManageBoardCards]}>
                <Menu.SubMenu
                    id='moveToBoard'
                    icon={<CompassIcon icon='arrow-right'/>}
                    name={intl.formatMessage({id: 'CardActionsMenu.moveToBoard', defaultMessage: 'Move to board'})}
                    position='left'
                >
                    {moveToBoards.map((board) => (
                        <Menu.Text
                            key={board.id}
                            id={board.id}
                            name={board.title || intl.formatMessage({id: 'ViewTitle.untitled-board', defaultMessage: 'Untitled board'})}
                            onClick={() => props.onClickMoveToBoard?.(board.id)}
                        />
                    ))}
                </Menu.SubMenu>
            </BoardPermissionGate>}
            {me?.id !== 'single-user' &&
                <Menu.Text
                    icon={<LinkIcon/>}
                    id='copy'
                    name={intl.formatMessage({id: 'CardActionsMenu.copyLink', defaultMessage: 'Copy link'})}
                    onClick={() => {
                        let cardLink = window.location.href

                        if (!cardLink.includes(cardId)) {
                            cardLink += `/${cardId}`
                        }

                        Utils.copyTextToClipboard(cardLink)
                        sendFlashMessage({content: intl.formatMessage({id: 'CardActionsMenu.copiedLink', defaultMessage: 'Copied!'}), severity: 'high'})
                    }}
                />
            }
            {props.children}
        </Menu>
    )
}

export default CardActionsMenu
