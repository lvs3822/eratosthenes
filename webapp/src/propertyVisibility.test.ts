// Copyright (c) 2015-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.
import {IPropertyTemplate, PropertyTypeEnum} from './blocks/board'
import {Card} from './blocks/card'
import {findVisibilityIssues, isPropertyVisible, visibleProperties} from './propertyVisibility'
import {TestBlockFactory} from './test/testBlockFactory'

describe('src/propertyVisibility', () => {
    const board = TestBlockFactory.createBoard()
    board.id = '1'

    // A property that a condition can point at. Its options are named after their
    // own ids, so the tests read as "status is blocked" rather than "status is
    // option-3".
    const source = (id: string, optionIds: string[], type: PropertyTypeEnum = 'select'): IPropertyTemplate => ({
        id,
        name: id,
        type,
        options: optionIds.map((optionId) => ({id: optionId, value: optionId, color: 'propColorBrown'})),
    })

    // A text property shown only when sourceId holds one of optionIds.
    const dependent = (id: string, sourceId: string, optionIds: string[]): IPropertyTemplate => ({
        id,
        name: id,
        type: 'text',
        options: [],
        visibleWhen: {propertyId: sourceId, optionIds},
    })

    // A source property that is itself conditional, for building chains.
    const dependentSource = (id: string, optionIds: string[], sourceId: string, sourceOptionIds: string[]): IPropertyTemplate => ({
        ...source(id, optionIds),
        visibleWhen: {propertyId: sourceId, optionIds: sourceOptionIds},
    })

    const cardWith = (properties: Record<string, string | string[]>): Card => {
        const card = TestBlockFactory.createCard(board)
        card.fields.properties = properties
        return card
    }

    describe('verify isPropertyVisible method', () => {
        test('should be true for a property with no condition', () => {
            const plain = source('plain', [])
            expect(isPropertyVisible(plain, cardWith({}), [plain])).toBeTruthy()
        })

        test('should be true when the card value matches the condition', () => {
            const status = source('status', ['todo', 'blocked'])
            const reason = dependent('reason', 'status', ['blocked'])
            expect(isPropertyVisible(reason, cardWith({status: 'blocked'}), [status, reason])).toBeTruthy()
        })

        test('should be false when the card value does not match the condition', () => {
            const status = source('status', ['todo', 'blocked'])
            const reason = dependent('reason', 'status', ['blocked'])
            expect(isPropertyVisible(reason, cardWith({status: 'todo'}), [status, reason])).toBeFalsy()
        })

        test('should be false for an empty card value', () => {
            const status = source('status', ['todo', 'blocked'])
            const reason = dependent('reason', 'status', ['blocked'])
            const templates = [status, reason]

            // Key absent, empty string, and empty array all match nothing.
            expect(isPropertyVisible(reason, cardWith({}), templates)).toBeFalsy()
            expect(isPropertyVisible(reason, cardWith({status: ''}), templates)).toBeFalsy()
            expect(isPropertyVisible(reason, cardWith({status: []}), templates)).toBeFalsy()
        })

        test('should match any of the selected options of a multiSelect source', () => {
            const tags = source('tags', ['urgent', 'blocked', 'later'], 'multiSelect')
            const reason = dependent('reason', 'tags', ['blocked'])
            const templates = [tags, reason]

            expect(isPropertyVisible(reason, cardWith({tags: ['urgent', 'blocked']}), templates)).toBeTruthy()
            expect(isPropertyVisible(reason, cardWith({tags: ['urgent', 'later']}), templates)).toBeFalsy()
        })

        test('should be true when the source property no longer exists', () => {
            const reason = dependent('reason', 'deleted-property-id', ['blocked'])
            expect(isPropertyVisible(reason, cardWith({}), [reason])).toBeTruthy()
        })

        test('should be true when the source property is not a select or multiSelect', () => {
            const notes = source('notes', [], 'text')
            const reason = dependent('reason', 'notes', ['blocked'])
            expect(isPropertyVisible(reason, cardWith({notes: 'blocked'}), [notes, reason])).toBeTruthy()
        })

        test('should be true when the condition names no options at all', () => {
            const status = source('status', ['todo', 'blocked'])
            const reason = dependent('reason', 'status', [])
            expect(isPropertyVisible(reason, cardWith({status: 'todo'}), [status, reason])).toBeTruthy()
        })

        test('should be true when every referenced option has been deleted', () => {
            const status = source('status', ['todo'])
            const reason = dependent('reason', 'status', ['blocked', 'on-hold'])
            expect(isPropertyVisible(reason, cardWith({status: 'todo'}), [status, reason])).toBeTruthy()
        })

        test('should evaluate against the surviving options when only some were deleted', () => {
            const status = source('status', ['todo', 'blocked'])
            const reason = dependent('reason', 'status', ['on-hold', 'blocked'])
            const templates = [status, reason]

            expect(isPropertyVisible(reason, cardWith({status: 'blocked'}), templates)).toBeTruthy()
            expect(isPropertyVisible(reason, cardWith({status: 'todo'}), templates)).toBeFalsy()
        })

        test('should hide a whole chain when a middle link is unsatisfied', () => {
            const status = source('status', ['todo', 'review'])
            const reviewType = dependentSource('reviewType', ['code', 'design'], 'status', ['review'])
            const reviewer = dependent('reviewer', 'reviewType', ['code'])
            const templates = [status, reviewType, reviewer]

            expect(isPropertyVisible(reviewer, cardWith({status: 'review', reviewType: 'code'}), templates)).toBeTruthy()

            // The card still holds reviewType = code, but reviewType is itself
            // hidden now, so reviewer must be hidden too.
            expect(isPropertyVisible(reviewType, cardWith({status: 'todo', reviewType: 'code'}), templates)).toBeFalsy()
            expect(isPropertyVisible(reviewer, cardWith({status: 'todo', reviewType: 'code'}), templates)).toBeFalsy()
        })

        test('should treat both properties of a two node cycle as visible', () => {
            const alpha = dependentSource('alpha', ['a'], 'beta', ['b'])
            const beta = dependentSource('beta', ['b'], 'alpha', ['a'])
            const templates = [alpha, beta]

            expect(isPropertyVisible(alpha, cardWith({}), templates)).toBeTruthy()
            expect(isPropertyVisible(beta, cardWith({}), templates)).toBeTruthy()
            expect(isPropertyVisible(alpha, cardWith({alpha: 'a', beta: 'b'}), templates)).toBeTruthy()
        })

        test('should treat a self referencing property as visible', () => {
            const loop = dependentSource('loop', ['x'], 'loop', ['x'])
            expect(isPropertyVisible(loop, cardWith({}), [loop])).toBeTruthy()
        })

        test('should evaluate a property downstream of a cycle normally', () => {
            const alpha = dependentSource('alpha', ['x', 'y'], 'beta', ['b'])
            const beta = dependentSource('beta', ['b'], 'alpha', ['x'])
            const downstream = dependent('downstream', 'alpha', ['x'])
            const templates = [alpha, beta, downstream]

            // alpha and beta fail open, but the blanket pass stops there.
            expect(isPropertyVisible(downstream, cardWith({alpha: 'x'}), templates)).toBeTruthy()
            expect(isPropertyVisible(downstream, cardWith({alpha: 'y'}), templates)).toBeFalsy()
        })
    })

    describe('verify visibleProperties method', () => {
        test('should return every property in board order when nothing is conditional', () => {
            const templates = [source('one', []), source('two', []), source('three', [])]
            expect(visibleProperties(cardWith({}), templates).map((o) => o.id)).toEqual(['one', 'two', 'three'])
        })

        test('should drop only the properties whose condition is unsatisfied, keeping board order', () => {
            const status = source('status', ['todo', 'review', 'blocked'])
            const reason = dependent('reason', 'status', ['blocked'])
            const reviewType = dependentSource('reviewType', ['code'], 'status', ['review'])
            const reviewer = dependent('reviewer', 'reviewType', ['code'])
            const templates = [status, reason, reviewType, reviewer]

            expect(visibleProperties(cardWith({status: 'blocked'}), templates).map((o) => o.id)).toEqual(['status', 'reason'])
            expect(visibleProperties(cardWith({status: 'review', reviewType: 'code'}), templates).map((o) => o.id)).toEqual(['status', 'reviewType', 'reviewer'])
            expect(visibleProperties(cardWith({status: 'todo'}), templates).map((o) => o.id)).toEqual(['status'])
        })

        test('should resolve a long chain without recursing', () => {
            const templates: IPropertyTemplate[] = [source('link0', ['on'])]
            for (let i = 1; i < 10000; i++) {
                templates.push(dependentSource(`link${i}`, ['on'], `link${i - 1}`, ['on']))
            }

            const card = cardWith(Object.fromEntries(templates.map((o) => [o.id, 'on'])))

            // Resolving the last link first walks all 10000 hops in one go, with
            // nothing memoised. A recursive resolver would overflow the stack here.
            expect(isPropertyVisible(templates[templates.length - 1], card, templates)).toBeTruthy()
            expect(visibleProperties(card, templates)).toHaveLength(10000)
        })
    })

    describe('verify findVisibilityIssues method', () => {
        test('should return nothing for a schema with no conditions', () => {
            expect(findVisibilityIssues([source('one', []), source('two', ['a'])])).toEqual([])
        })

        test('should return nothing for a schema whose conditions are all sound', () => {
            const status = source('status', ['todo', 'blocked'])
            const reason = dependent('reason', 'status', ['blocked'])
            expect(findVisibilityIssues([status, reason])).toEqual([])
        })

        test('should not report a condition that names no options', () => {
            const status = source('status', ['todo'])
            const reason = dependent('reason', 'status', [])
            expect(findVisibilityIssues([status, reason])).toEqual([])
        })

        test('should detect a missing source property', () => {
            const reason = dependent('reason', 'deleted-property-id', ['blocked'])
            expect(findVisibilityIssues([reason])).toEqual([
                {kind: 'missingSource', propertyId: 'reason', sourcePropertyId: 'deleted-property-id'},
            ])
        })

        test('should detect a source property of the wrong type', () => {
            const notes = source('notes', [], 'text')
            const reason = dependent('reason', 'notes', ['blocked'])
            expect(findVisibilityIssues([notes, reason])).toEqual([
                {kind: 'invalidSourceType', propertyId: 'reason', sourcePropertyId: 'notes', sourceType: 'text'},
            ])
        })

        test('should detect deleted options, including when only some are gone', () => {
            const status = source('status', ['todo', 'blocked'])
            const reason = dependent('reason', 'status', ['on-hold', 'blocked'])
            expect(findVisibilityIssues([status, reason])).toEqual([
                {kind: 'missingOptions', propertyId: 'reason', sourcePropertyId: 'status', missingOptionIds: ['on-hold']},
            ])
        })

        test('should detect a cycle once, rotated to its lowest indexed member', () => {
            // The detector enters the cycle from entry, so it first meets beta.
            // Rotation puts alpha first because it comes earlier on the board.
            const entry = dependent('entry', 'beta', ['b'])
            const alpha = dependentSource('alpha', ['a'], 'beta', ['b'])
            const beta = dependentSource('beta', ['b'], 'alpha', ['a'])

            expect(findVisibilityIssues([entry, alpha, beta])).toEqual([
                {kind: 'cycle', propertyIds: ['alpha', 'beta']},
            ])
        })

        test('should not report a cycle when a broken link stops the chain', () => {
            // alpha points at beta, beta points back at alpha, but every option
            // beta references is gone, so the resolver never loops and neither
            // does the detector.
            const alpha = dependentSource('alpha', ['a'], 'beta', ['b'])
            const beta = dependentSource('beta', ['b'], 'alpha', ['deleted-option'])

            expect(findVisibilityIssues([alpha, beta])).toEqual([
                {kind: 'missingOptions', propertyId: 'beta', sourcePropertyId: 'alpha', missingOptionIds: ['deleted-option']},
            ])
        })

        test('should return several distinct issues in a stable sorted order', () => {
            const status = source('status', ['todo'])
            const notes = source('notes', [], 'text')
            const templates = [
                dependent('missingOpt', 'status', ['gone', 'todo']),
                status,
                dependent('orphan', 'deleted-property-id', ['todo']),
                notes,
                dependent('wrongType', 'notes', ['todo']),
                dependentSource('alpha', ['a'], 'beta', ['b']),
                dependentSource('beta', ['b'], 'alpha', ['a']),
            ]

            const expected = [
                {kind: 'missingOptions', propertyId: 'missingOpt', sourcePropertyId: 'status', missingOptionIds: ['gone']},
                {kind: 'missingSource', propertyId: 'orphan', sourcePropertyId: 'deleted-property-id'},
                {kind: 'invalidSourceType', propertyId: 'wrongType', sourcePropertyId: 'notes', sourceType: 'text'},
                {kind: 'cycle', propertyIds: ['alpha', 'beta']},
            ]

            expect(findVisibilityIssues(templates)).toEqual(expected)

            // Stable across calls, so the editor's warning list does not reshuffle.
            expect(findVisibilityIssues(templates)).toEqual(expected)
        })
    })
})
