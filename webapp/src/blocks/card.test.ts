// Copyright (c) 2015-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

import {Block, createPatchesFromBlocks} from './block'

import {Card, RecurrenceConfig, createCard, isRecurrenceActive} from './card'

const recurrence = (): RecurrenceConfig => ({
    enabled: true,
    mode: 'schedule',
    groupPropertyId: 'status-id',
    targetOptionId: 'opt-todo',
    timezone: 'Europe/Moscow',
    time: '09:00',
    historyMode: 'newInstance',
    startAt: 1767225600000,
    rule: {
        kind: 'weekly',
        interval: 2,
        weekdays: [1, 3, 5],
    },
})

// eslint-disable-next-line @typescript-eslint/no-explicit-any
const cardBlock = (fields: Record<string, any>): Block => ({
    id: 'card-id',
    boardId: 'board-id',
    parentId: 'board-id',
    createdBy: 'user-id',
    modifiedBy: 'user-id',
    schema: 1,
    type: 'card',
    title: 'Water the plants',
    fields: {
        icon: '🪴',
        properties: {'status-id': 'opt-done'},
        contentOrder: [],
        isTemplate: false,
        ...fields,
    },
    createAt: 1000,
    updateAt: 1000,
    deleteAt: 0,
})

describe('card tests', () => {
    describe('createCard preserves the recurrence fields', () => {
        it('should preserve cardType and recurrence through the clone', () => {
            const card = createCard(cardBlock({cardType: 'recurring', recurrence: recurrence()}))

            expect(card.fields.cardType).toBe('recurring')
            expect(card.fields.recurrence).toEqual(recurrence())
        })

        it('should deep clone recurrence, so mutating the clone leaves the source untouched', () => {
            const source = cardBlock({cardType: 'recurring', recurrence: recurrence()})

            const cloned = createCard(source).fields.recurrence!
            cloned.enabled = false
            cloned.rule!.interval = 99
            cloned.rule!.weekdays!.push(7)

            expect(cloned).not.toBe(source.fields.recurrence)
            expect(cloned.rule).not.toBe(source.fields.recurrence.rule)
            expect(source.fields.recurrence).toEqual(recurrence())
        })

        it('should not emit either key when the card is not recurring', () => {
            const card = createCard(cardBlock({}))

            expect('cardType' in card.fields).toBe(false)
            expect('recurrence' in card.fields).toBe(false)
            expect(Object.keys(card.fields).sort()).toEqual(['contentOrder', 'icon', 'isTemplate', 'properties'])
        })

        it('should not delete recurrence on the next patch after a clone', () => {
            // createPatchesFromBlocks puts every key present on the old block and
            // missing from the new one into the redo patch's deletedFields. A field
            // the clone drops is therefore not merely lost on the client, it is
            // erased on the server the next time the card is edited.
            const stored = cardBlock({cardType: 'recurring', recurrence: recurrence()})
            const edited = createCard(stored)
            edited.title = 'Water the plants twice'

            const [redo, undo] = createPatchesFromBlocks(edited, stored)

            expect(redo.deletedFields).not.toContain('recurrence')
            expect(redo.deletedFields).not.toContain('cardType')
            expect(undo.deletedFields).not.toContain('recurrence')
            expect(undo.deletedFields).not.toContain('cardType')
        })

        it('should keep an existing card without recurrence byte identical', () => {
            const stored = cardBlock({})
            const edited = createCard(stored)

            const [redo] = createPatchesFromBlocks(edited, stored)

            expect(redo.deletedFields).toEqual([])
            expect(Object.keys(redo.updatedFields || {})).not.toContain('recurrence')
            expect(Object.keys(redo.updatedFields || {})).not.toContain('cardType')
        })
    })

    describe('isRecurrenceActive', () => {
        const cardWith = (fields: Partial<Card['fields']>): Card =>
            createCard(cardBlock(fields))

        it('should be true only for a recurring card whose recurrence is enabled', () => {
            expect(isRecurrenceActive(cardWith({cardType: 'recurring', recurrence: recurrence()}))).toBe(true)
        })

        it('should be false for a recurring card that is paused', () => {
            expect(isRecurrenceActive(cardWith({
                cardType: 'recurring',
                recurrence: {...recurrence(), enabled: false},
            }))).toBe(false)
        })

        it('should be false for a recurring card with no configuration', () => {
            expect(isRecurrenceActive(cardWith({cardType: 'recurring'}))).toBe(false)
        })

        it('should be false for a normal card with a leftover enabled configuration', () => {
            expect(isRecurrenceActive(cardWith({cardType: 'normal', recurrence: recurrence()}))).toBe(false)
        })

        it('should be false for a card with neither field', () => {
            expect(isRecurrenceActive(cardWith({}))).toBe(false)
        })
    })
})
