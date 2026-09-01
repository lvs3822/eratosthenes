// Copyright (c) 2015-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.
import React from 'react'
import {render} from '@testing-library/react'
import {Provider as ReduxProvider} from 'react-redux'

import {TestBlockFactory} from '../../test/testBlockFactory'
import '@testing-library/jest-dom'
import {wrapIntl, mockStateStore} from '../../testUtils'
import {IPropertyTemplate} from '../../blocks/board'

import CalendarView from './fullCalendar'

jest.mock('../../mutator')

describe('components/calendar/toolbar', () => {
    const mockShow = jest.fn()
    const mockAdd = jest.fn()
    const dateDisplayProperty = {
        id: '12345',
        name: 'DateProperty',
        type: 'date',
        options: [],
    } as IPropertyTemplate
    const board = TestBlockFactory.createBoard()
    const view = TestBlockFactory.createBoardView(board)
    view.fields.viewType = 'calendar'
    view.fields.groupById = undefined
    const card = TestBlockFactory.createCard(board)
    const fifth = Date.UTC(2021, 9, 5, 12)
    const twentieth = Date.UTC(2021, 9, 20, 12)
    card.createAt = fifth
    const rObject = {from: twentieth}

    const state = {
        teams: {
            current: {id: 'team-id'},
        },
        boards: {
            current: board.id,
            boards: {
                [board.id]: board,
            },
            myBoardMemberships: {
                [board.id]: {userId: 'user_id_1', schemeAdmin: true},
            },
        },
    }
    const store = mockStateStore([], state)
    beforeEach(() => {
        jest.clearAllMocks()
    })

    test('return calendar, no date property', () => {
        const {container} = render(
            wrapIntl(
                <ReduxProvider store={store}>
                    <CalendarView
                        board={board}
                        activeView={view}
                        cards={[card]}
                        readonly={false}
                        showCard={mockShow}
                        addCard={mockAdd}
                        initialDate={new Date(fifth)}
                    />
                </ReduxProvider>,
            ),
        )
        expect(container).toMatchSnapshot()
    })

    test('return calendar, with date property not set', () => {
        board.cardProperties.push(dateDisplayProperty)
        card.fields.properties['12345'] = JSON.stringify(rObject)
        const {container} = render(
            wrapIntl(
                <ReduxProvider store={store}>
                    <CalendarView
                        board={board}
                        activeView={view}
                        cards={[card]}
                        readonly={false}
                        showCard={mockShow}
                        addCard={mockAdd}
                        initialDate={new Date(fifth)}
                    />
                </ReduxProvider>,
            ),
        )
        expect(container).toMatchSnapshot()
    })

    test('return calendar, with date property set', () => {
        board.cardProperties.push(dateDisplayProperty)
        card.fields.properties['12345'] = JSON.stringify(rObject)
        const {container} = render(
            wrapIntl(
                <ReduxProvider store={store}>
                    <CalendarView
                        board={board}
                        activeView={view}
                        readonly={false}
                        dateDisplayProperty={dateDisplayProperty}
                        cards={[card]}
                        showCard={mockShow}
                        addCard={mockAdd}
                        initialDate={new Date(fifth)}
                    />
                </ReduxProvider>,
            ),
        )
        expect(container).toMatchSnapshot()
    })

    test('return calendar, without permissions', () => {
        const localStore = mockStateStore([], {...state, teams: {current: undefined}})
        const {container} = render(
            wrapIntl(
                <ReduxProvider store={localStore}>
                    <CalendarView
                        board={board}
                        activeView={view}
                        cards={[card]}
                        readonly={false}
                        showCard={mockShow}
                        addCard={mockAdd}
                        initialDate={new Date(fifth)}
                    />
                </ReduxProvider>,
            ),
        )
        expect(container).toMatchSnapshot()
    })

    describe('visibility conditions', () => {
        const conditionalBoard = TestBlockFactory.createBoard()
        conditionalBoard.cardProperties = [
            {
                id: 'status_id',
                name: 'Status',
                type: 'select',
                options: [
                    {color: 'propColorDefault', id: 'status_todo', value: 'Todo'},
                    {color: 'propColorDefault', id: 'status_blocked', value: 'Blocked'},
                ],
            },
            {
                id: 'reason_id',
                name: 'Blocked reason',
                type: 'text',
                options: [],
                visibleWhen: {propertyId: 'status_id', optionIds: ['status_blocked']},
            },
        ]

        const conditionalView = TestBlockFactory.createBoardView(conditionalBoard)
        conditionalView.fields.viewType = 'calendar'
        conditionalView.fields.groupById = undefined
        conditionalView.fields.visiblePropertyIds = ['status_id', 'reason_id']

        const conditionalStore = mockStateStore([], {
            teams: {current: {id: 'team-id'}},
            boards: {
                current: conditionalBoard.id,
                boards: {[conditionalBoard.id]: conditionalBoard},
                myBoardMemberships: {[conditionalBoard.id]: {userId: 'user_id_1', schemeAdmin: true}},
            },
        })

        function renderWithStatus(status: string) {
            const conditionalCard = TestBlockFactory.createCard(conditionalBoard)
            conditionalCard.createAt = fifth
            conditionalCard.fields.properties.status_id = status

            return render(
                wrapIntl(
                    <ReduxProvider store={conditionalStore}>
                        <CalendarView
                            board={conditionalBoard}
                            activeView={conditionalView}
                            cards={[conditionalCard]}
                            readonly={false}
                            showCard={mockShow}
                            addCard={mockAdd}
                            initialDate={new Date(fifth)}
                        />
                    </ReduxProvider>,
                ),
            )
        }

        test('should hide a card property whose condition is not met', () => {
            const {container} = renderWithStatus('status_todo')
            expect(container.querySelector('[data-tooltip="Status"]')).not.toBeNull()
            expect(container.querySelector('[data-tooltip="Blocked reason"]')).toBeNull()
        })

        test('should show the card property once the condition is met', () => {
            const {container} = renderWithStatus('status_blocked')
            expect(container.querySelector('[data-tooltip="Status"]')).not.toBeNull()
            expect(container.querySelector('[data-tooltip="Blocked reason"]')).not.toBeNull()
        })
    })
})
