// Copyright (c) 2015-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.
import React from 'react'
import {render, screen, fireEvent} from '@testing-library/react'
import {mocked} from 'jest-mock'
import '@testing-library/jest-dom'

import mutator from '../../mutator'
import {wrapIntl} from '../../testUtils'
import {TestBlockFactory} from '../../test/testBlockFactory'
import {Board, IPropertyTemplate} from '../../blocks/board'
import Menu from '../../widgets/menu'

import ColumnPropertiesMenu from './columnPropertiesMenu'

jest.mock('../../mutator')
const mockedMutator = mocked(mutator, true)

describe('components/kanban/ColumnPropertiesMenu', () => {
    const status: IPropertyTemplate = {
        id: 'status_id',
        name: 'Status',
        type: 'select',
        options: [
            {id: 'todo', value: 'Todo', color: 'propColorDefault'},
            {id: 'blocked', value: 'Blocked', color: 'propColorDefault'},
        ],
    }

    const boardWith = (cardProperties: IPropertyTemplate[]): Board => {
        const board = TestBlockFactory.createBoard()
        board.cardProperties = cardProperties
        return board
    }

    // The component renders a Menu.SubMenu, which only reveals its children on
    // hover, so every test opens it the same way.
    function renderMenu(board: Board, groupByProperty?: IPropertyTemplate, optionId = 'blocked') {
        const result = render(wrapIntl(
            <Menu>
                <ColumnPropertiesMenu
                    board={board}
                    groupByProperty={groupByProperty}
                    optionId={optionId}
                />
            </Menu>,
        ))

        const entry = screen.queryByText('Properties for this column')
        if (entry) {
            fireEvent.mouseOver(entry)
        }

        return result
    }

    beforeEach(jest.resetAllMocks)

    test('should be disabled when the board is not grouped by a select property', () => {
        renderMenu(boardWith([status]), undefined)

        expect(screen.getByText('Properties for this column')).toBeInTheDocument()
        expect(screen.getByText('Available when the board is grouped by a Select property')).toBeInTheDocument()
    })

    test('should put a property conditioned on this column in the scoped section, checked', () => {
        const reason: IPropertyTemplate = {
            id: 'reason_id',
            name: 'Blocked reason',
            type: 'text',
            options: [],
            visibleWhen: {propertyId: 'status_id', optionIds: ['blocked', 'todo']},
        }

        const {container} = renderMenu(boardWith([status, reason]), status)

        expect(screen.getByText('Shown only in some columns')).toBeInTheDocument()
        expect(container.querySelector('.SwitchOption[aria-label="Blocked reason"]')).toBeInTheDocument()
    })

    test('should add this column option when a property is checked', () => {
        const reason: IPropertyTemplate = {
            id: 'reason_id',
            name: 'Blocked reason',
            type: 'text',
            options: [],
            visibleWhen: {propertyId: 'status_id', optionIds: ['todo']},
        }
        const board = boardWith([status, reason])

        const {container} = renderMenu(board, status)
        fireEvent.click(container.querySelector('.SwitchOption[aria-label="Blocked reason"]')!)

        expect(mockedMutator.changePropertyVisibility).toHaveBeenCalledWith(
            board.id,
            board.cardProperties,
            reason,
            {propertyId: 'status_id', optionIds: ['todo', 'blocked']},
        )
    })

    test('should remove this column option when a property is unchecked', () => {
        const reason: IPropertyTemplate = {
            id: 'reason_id',
            name: 'Blocked reason',
            type: 'text',
            options: [],
            visibleWhen: {propertyId: 'status_id', optionIds: ['todo', 'blocked']},
        }
        const board = boardWith([status, reason])

        const {container} = renderMenu(board, status)
        fireEvent.click(container.querySelector('.SwitchOption[aria-label="Blocked reason"]')!)

        expect(mockedMutator.changePropertyVisibility).toHaveBeenCalledWith(
            board.id,
            board.cardProperties,
            reason,
            {propertyId: 'status_id', optionIds: ['todo']},
        )
    })

    test('should block the last uncheck rather than ejecting the property into the read-only section', () => {
        const reason: IPropertyTemplate = {
            id: 'reason_id',
            name: 'Blocked reason',
            type: 'text',
            options: [],
            visibleWhen: {propertyId: 'status_id', optionIds: ['blocked']},
        }
        const board = boardWith([status, reason])

        const {container} = renderMenu(board, status)
        const option = container.querySelector('.SwitchOption[aria-label="Blocked reason"]')

        expect(option).toHaveClass('menu-option--disabled')

        fireEvent.click(option!)
        expect(mockedMutator.changePropertyVisibility).not.toHaveBeenCalled()
    })

    test('should list an unconditional property as always visible and not as a switch', () => {
        const owner: IPropertyTemplate = {id: 'owner_id', name: 'Owner', type: 'text', options: []}

        const {container} = renderMenu(boardWith([status, owner]), status)

        expect(screen.getByText('Always visible')).toBeInTheDocument()
        expect(screen.getByText('Owner')).toBeInTheDocument()
        expect(container.querySelector('.SwitchOption[aria-label="Owner"]')).toBeNull()
    })

    test('should treat a condition with no options as always visible', () => {
        // Empty optionIds means "no constraint", so the property shows in every
        // column and belongs with the others that do.
        const half: IPropertyTemplate = {
            id: 'half_id',
            name: 'Half configured',
            type: 'text',
            options: [],
            visibleWhen: {propertyId: 'status_id', optionIds: []},
        }

        const {container} = renderMenu(boardWith([status, half]), status)

        expect(screen.getByText('Always visible')).toBeInTheDocument()
        expect(container.querySelector('.SwitchOption[aria-label="Half configured"]')).toBeNull()
    })

    test('should omit a property conditioned on a different property', () => {
        const priority: IPropertyTemplate = {
            id: 'priority_id',
            name: 'Priority',
            type: 'select',
            options: [{id: 'high', value: 'High', color: 'propColorDefault'}],
        }
        const escalation: IPropertyTemplate = {
            id: 'escalation_id',
            name: 'Escalation path',
            type: 'text',
            options: [],
            visibleWhen: {propertyId: 'priority_id', optionIds: ['high']},
        }

        renderMenu(boardWith([status, priority, escalation]), status)

        // Not shown at all, but still unreachable: this screen cannot construct a
        // condition the property menu would refuse.
        expect(screen.queryByText('Escalation path')).toBeNull()
    })

    test('should omit the group-by property itself, which would be a self reference', () => {
        const {container} = renderMenu(boardWith([status]), status)

        expect(container.querySelector('.SwitchOption[aria-label="Status"]')).toBeNull()
    })

    test('should omit a property the group-by property already depends on', () => {
        // Status depends on Phase, so conditioning Phase on Status would close a
        // loop. The property menu refuses this; so must this screen.
        const phase: IPropertyTemplate = {
            id: 'phase_id',
            name: 'Phase',
            type: 'select',
            options: [{id: 'early', value: 'Early', color: 'propColorDefault'}],
        }
        const cyclicStatus: IPropertyTemplate = {
            ...status,
            visibleWhen: {propertyId: 'phase_id', optionIds: ['early']},
        }

        const {container} = renderMenu(boardWith([cyclicStatus, phase]), cyclicStatus)

        expect(container.querySelector('.SwitchOption[aria-label="Phase"]')).toBeNull()
    })
})
