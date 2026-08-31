// Copyright (c) 2015-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.
import {TestBlockFactory} from '../test/testBlockFactory'

import {createPatchesFromBoards, createBoard, IPropertyTemplate, createPatchesFromBoardsAndBlocks} from './board'
import {createBlock} from './block'

describe('board tests', () => {
    describe('correctly generate patches from two boards', () => {
        it('should generate two empty patches for the same board', () => {
            const board = TestBlockFactory.createBoard()
            const result = createPatchesFromBoards(board, board)
            expect(result).toMatchSnapshot()
        })

        it('should add properties on the update patch and remove them on the undo', () => {
            const board = TestBlockFactory.createBoard()
            board.properties = {
                prop1: 'val1',
                prop2: 'val2',
            }
            const oldBoard = createBoard(board)
            oldBoard.properties = {
                prop2: 'val2',
            }

            const result = createPatchesFromBoards(board, oldBoard)
            expect(result).toMatchSnapshot()
        })

        it('should add card properties on the redo and remove them on the undo', () => {
            const board = TestBlockFactory.createBoard()
            const oldBoard = createBoard(board)
            board.cardProperties.push({
                id: 'new-property-id',
                name: 'property-name',
                type: 'select',
                options: [{
                    id: 'opt',
                    value: 'val',
                    color: 'propColorYellow',
                }],
            })

            const result = createPatchesFromBoards(board, oldBoard)
            expect(result).toMatchSnapshot()
        })

        it('should add card properties on the redo and undo if they exists in both, but differ', () => {
            const cardProperty = {
                id: 'new-property-id',
                name: 'property-name',
                type: 'select',
                options: [{
                    id: 'opt',
                    value: 'val',
                    color: 'propColorYellow',
                }],
            } as IPropertyTemplate

            const board = TestBlockFactory.createBoard()
            const oldBoard = createBoard(board)
            board.cardProperties = [cardProperty]
            oldBoard.cardProperties = [{...cardProperty, name: 'a-different-name'}]

            const result = createPatchesFromBoards(board, oldBoard)
            expect(result).toMatchSnapshot()
        })

        it('should add card properties on the redo and undo if they exists in both, but their options are different', () => {
            const cardProperty = {
                id: 'new-property-id',
                name: 'property-name',
                type: 'select',
                options: [{
                    id: 'opt',
                    value: 'val',
                    color: 'propColorYellow',
                }],
            } as IPropertyTemplate

            const board = TestBlockFactory.createBoard()
            const oldBoard = createBoard(board)
            board.cardProperties = [cardProperty]
            oldBoard.cardProperties = [{
                ...cardProperty,
                options: [{
                    id: 'another-opt',
                    value: 'val',
                    color: 'propColorBrown',
                }],
            }]

            const result = createPatchesFromBoards(board, oldBoard)
            expect(result).toMatchSnapshot()
        })

        it('should preserve visibleWhen through the createBoard clone', () => {
            const board = TestBlockFactory.createBoard()
            board.cardProperties = [{
                id: 'reason-id',
                name: 'Blocked reason',
                type: 'text',
                options: [],
                visibleWhen: {
                    propertyId: 'status-id',
                    optionIds: ['opt-blocked', 'opt-on-hold'],
                },
            }]

            const clonedBoard = createBoard(board)

            expect(clonedBoard.cardProperties[0].visibleWhen).toEqual({
                propertyId: 'status-id',
                optionIds: ['opt-blocked', 'opt-on-hold'],
            })
        })

        it('should deep clone visibleWhen, so mutating the clone leaves the source untouched', () => {
            const sourceProperty: IPropertyTemplate = {
                id: 'reason-id',
                name: 'Blocked reason',
                type: 'text',
                options: [],
                visibleWhen: {
                    propertyId: 'status-id',
                    optionIds: ['opt-blocked'],
                },
            }

            const board = TestBlockFactory.createBoard()
            board.cardProperties = [sourceProperty]

            const clonedVisibility = createBoard(board).cardProperties[0].visibleWhen!
            clonedVisibility.propertyId = 'another-property-id'
            clonedVisibility.optionIds.push('opt-on-hold')

            expect(clonedVisibility).not.toBe(sourceProperty.visibleWhen)
            expect(sourceProperty.visibleWhen).toEqual({
                propertyId: 'status-id',
                optionIds: ['opt-blocked'],
            })
        })

        it('should not emit a visibleWhen key at all when the source property has no condition', () => {
            const board = TestBlockFactory.createBoard()
            board.cardProperties = [{
                id: 'plain-id',
                name: 'Plain property',
                type: 'text',
                options: [],
            }]

            const clonedProperty = createBoard(board).cardProperties[0]

            expect('visibleWhen' in clonedProperty).toBe(false)
            expect(Object.keys(clonedProperty)).not.toContain('visibleWhen')
        })

        it('should not generate a card property patch when visibleWhen is equal, including a reordered optionIds', () => {
            const cardProperty: IPropertyTemplate = {
                id: 'reason-id',
                name: 'Blocked reason',
                type: 'text',
                options: [],
                visibleWhen: {
                    propertyId: 'status-id',
                    optionIds: ['opt-blocked', 'opt-on-hold'],
                },
            }

            const board = TestBlockFactory.createBoard()
            board.cardProperties = [cardProperty]

            // createBoard deep clones, so the two conditions are equal but are not
            // the same object. Compared by reference this marked the property as
            // updated on every single board patch.
            const oldBoard = createBoard(board)
            expect(createPatchesFromBoards(board, oldBoard)[0].updatedCardProperties).toEqual([])

            // optionIds is a set, so reordering it is not a change either.
            oldBoard.cardProperties[0].visibleWhen!.optionIds = ['opt-on-hold', 'opt-blocked']
            const result = createPatchesFromBoards(board, oldBoard)
            expect(result[0].updatedCardProperties).toEqual([])
            expect(result[1].updatedCardProperties).toEqual([])
        })

        it('should generate a card property patch when optionIds changed', () => {
            const cardProperty: IPropertyTemplate = {
                id: 'reason-id',
                name: 'Blocked reason',
                type: 'text',
                options: [],
                visibleWhen: {
                    propertyId: 'status-id',
                    optionIds: ['opt-blocked'],
                },
            }

            const board = TestBlockFactory.createBoard()
            board.cardProperties = [cardProperty]
            const oldBoard = createBoard(board)
            oldBoard.cardProperties[0].visibleWhen!.optionIds = ['opt-on-hold']

            const result = createPatchesFromBoards(board, oldBoard)
            expect(result[0].updatedCardProperties).toHaveLength(1)
            expect(result[0].updatedCardProperties![0].visibleWhen!.optionIds).toEqual(['opt-blocked'])
            expect(result[1].updatedCardProperties).toHaveLength(1)
            expect(result[1].updatedCardProperties![0].visibleWhen!.optionIds).toEqual(['opt-on-hold'])
        })

        it('should generate a card property patch when visibleWhen is added on one side, in both directions', () => {
            const plainProperty: IPropertyTemplate = {
                id: 'reason-id',
                name: 'Blocked reason',
                type: 'text',
                options: [],
            }
            const conditionalProperty: IPropertyTemplate = {
                ...plainProperty,
                visibleWhen: {
                    propertyId: 'status-id',
                    optionIds: ['opt-blocked'],
                },
            }

            const board = TestBlockFactory.createBoard()
            const oldBoard = createBoard(board)

            // Condition added.
            board.cardProperties = [conditionalProperty]
            oldBoard.cardProperties = [plainProperty]
            const added = createPatchesFromBoards(board, oldBoard)
            expect(added[0].updatedCardProperties).toHaveLength(1)
            expect(added[1].updatedCardProperties).toHaveLength(1)

            // Condition removed. This is the asymmetric case: the property whose
            // keys are walked one by one is the one WITHOUT visibleWhen, so the
            // difference is invisible to that loop.
            board.cardProperties = [plainProperty]
            oldBoard.cardProperties = [conditionalProperty]
            const removed = createPatchesFromBoards(board, oldBoard)
            expect(removed[0].updatedCardProperties).toHaveLength(1)
            expect(removed[1].updatedCardProperties).toHaveLength(1)
        })
    })

    describe('correctly generate patches for boards and blocks', () => {
        const board = TestBlockFactory.createBoard()
        board.id = 'test-board-id'
        const card = TestBlockFactory.createCard()
        card.id = 'test-card-id'

        it('should generate two empty patches for the same board and block', () => {
            const result = createPatchesFromBoardsAndBlocks(board, board, [card.id], [card], [card])
            expect(result).toMatchSnapshot()
        })

        it('should add fields on update and remove it in the undo', () => {
            const oldBlock = TestBlockFactory.createText(card)
            oldBlock.id = 'test-old-block-id'
            const newBlock = createBlock(oldBlock)
            newBlock.fields.newField = 'new field'

            const result = createPatchesFromBoardsAndBlocks(board, board, [newBlock.id], [newBlock], [oldBlock])
            expect(result).toMatchSnapshot()
        })
    })
})
