// Copyright (c) 2015-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

import {IPropertyTemplate, PropertyTypeEnum} from './blocks/board'
import {Card} from './blocks/card'

// A condition may only point at a property whose card values are option ids.
const conditionSourceTypes: PropertyTypeEnum[] = ['select', 'multiSelect']

// A problem with the visibility conditions of a board's card properties.
//
// Every kind listed here is a case the resolver deliberately fails open on, so
// none of them can hide anything. They exist so that the property editor can warn
// about a condition that silently does nothing, or that would otherwise make an
// already filled in property unreachable.
type VisibilityIssue =
    | {kind: 'cycle', propertyIds: string[]}
    | {kind: 'missingSource', propertyId: string, sourcePropertyId: string}
    | {kind: 'invalidSourceType', propertyId: string, sourcePropertyId: string, sourceType: PropertyTypeEnum}
    | {kind: 'missingOptions', propertyId: string, sourcePropertyId: string, missingOptionIds: string[]}

// The classification of a single condition. resolveCondition below is the only
// place that produces one, so the resolver and the diagnostic can never disagree
// about what counts as a broken condition.
type ResolvedCondition =
    | {kind: 'unconditional'}
    | {kind: 'missingSource', sourcePropertyId: string}
    | {kind: 'invalidSourceType', source: IPropertyTemplate}
    | {kind: 'conditional', source: IPropertyTemplate, validOptionIds: string[], missingOptionIds: string[]}

// Lookup tables shared by every property resolved in one call.
type VisibilityContext = {
    templatesById: Map<string, IPropertyTemplate>

    // Built lazily, because most boards have no conditions at all and would never
    // need it.
    optionIdsByTemplateId: Map<string, Set<string>>
}

function createContext(allTemplates: readonly IPropertyTemplate[]): VisibilityContext {
    const templatesById = new Map<string, IPropertyTemplate>()
    allTemplates.forEach((template) => templatesById.set(template.id, template))

    return {templatesById, optionIdsByTemplateId: new Map<string, Set<string>>()}
}

function optionIdsOf(context: VisibilityContext, template: IPropertyTemplate): Set<string> {
    const cached = context.optionIdsByTemplateId.get(template.id)
    if (cached) {
        return cached
    }

    // Defensive about options being absent: card properties arrive from the server
    // as untyped JSON.
    const optionIds = new Set((template.options || []).map((option) => option.id))
    context.optionIdsByTemplateId.set(template.id, optionIds)

    return optionIds
}

function resolveCondition(context: VisibilityContext, template: IPropertyTemplate): ResolvedCondition {
    const {visibleWhen} = template
    if (!visibleWhen || !visibleWhen.optionIds || visibleWhen.optionIds.length === 0) {
        // No condition at all, or a condition that names no options. An empty
        // optionIds is the state the editor sits in between picking a source
        // property and ticking the first option, so it means "no constraint"
        // rather than "never visible".
        return {kind: 'unconditional'}
    }

    const source = context.templatesById.get(visibleWhen.propertyId)
    if (!source) {
        return {kind: 'missingSource', sourcePropertyId: visibleWhen.propertyId}
    }

    if (!conditionSourceTypes.includes(source.type)) {
        return {kind: 'invalidSourceType', source}
    }

    const sourceOptionIds = optionIdsOf(context, source)
    const validOptionIds: string[] = []
    const missingOptionIds: string[] = []
    visibleWhen.optionIds.forEach((optionId) => {
        if (sourceOptionIds.has(optionId)) {
            validOptionIds.push(optionId)
        } else {
            missingOptionIds.push(optionId)
        }
    })

    return {kind: 'conditional', source, validOptionIds, missingOptionIds}
}

// The property this one depends on, or undefined when the chain stops here. Only
// a condition that can actually be evaluated forms an edge, so the cycle detector
// walks exactly the graph the resolver walks.
function conditionEdge(condition: ResolvedCondition): string | undefined {
    if (condition.kind === 'conditional' && condition.validOptionIds.length > 0) {
        return condition.source.id
    }

    return undefined
}

// Mirrors the 'includes' clause in cardFilter.ts, which covers select (a string)
// and multiSelect (an array of strings) with one expression. A card value that is
// absent, empty, or an empty array matches nothing.
function conditionMetByCard(card: Card, condition: ResolvedCondition): boolean {
    if (condition.kind !== 'conditional' || condition.validOptionIds.length === 0) {
        // Nothing evaluable, so nothing constrains the property.
        return true
    }

    const value = card.fields.properties[condition.source.id]

    return condition.validOptionIds.some((optionId) => (Array.isArray(value) ? value.includes(optionId) : optionId === value))
}

// Resolves one property and memoises every property it had to walk through.
//
// A property has at most one outgoing edge, so the dependency graph is a set of
// chains that either terminate or run into a cycle. The walk is iterative, so
// chain depth can never overflow the stack, and it stops the moment it reaches a
// property that is already in the memo.
function resolveVisibility(context: VisibilityContext, memo: Map<string, boolean>, card: Card, startId: string): boolean {
    const cached = memo.get(startId)
    if (cached !== undefined) {
        return cached
    }

    const path: string[] = []
    const conditions: ResolvedCondition[] = []
    const positions = new Map<string, number>()

    // Index in path of the deepest property whose visibility is now known. Every
    // property from here to the end of the path is in the memo.
    let resolvedFrom = 0
    let cursor = startId

    for (;;) {
        const condition = resolveCondition(context, context.templatesById.get(cursor)!)
        positions.set(cursor, path.length)
        path.push(cursor)
        conditions.push(condition)

        const nextId = conditionEdge(condition)
        if (nextId === undefined) {
            // Unconditional, or one of the three fail-open branches: no source
            // property, a source of the wrong type, or every referenced option
            // gone. Hiding here would put entered data out of reach.
            memo.set(cursor, true)
            resolvedFrom = path.length - 1
            break
        }

        const known = memo.get(nextId)
        if (known !== undefined) {
            memo.set(cursor, known && conditionMetByCard(card, condition))
            resolvedFrom = path.length - 1
            break
        }

        const seenAt = positions.get(nextId)
        if (seenAt !== undefined) {
            // A cycle. Every property from nextId to the end of the path is inside
            // it and fails open. Properties before nextId are merely downstream of
            // the cycle and are resolved normally on the way back out, so the
            // exception stays where it originated.
            for (let i = seenAt; i < path.length; i++) {
                memo.set(path[i], true)
            }
            resolvedFrom = seenAt
            break
        }

        cursor = nextId
    }

    for (let i = resolvedFrom - 1; i >= 0; i--) {
        const sourceVisible = memo.get(path[i + 1])!
        memo.set(path[i], sourceVisible && conditionMetByCard(card, conditions[i]))
    }

    return memo.get(startId)!
}

/**
 * Whether this card property should be shown on this card.
 *
 * NOTE: this builds its lookup index on every call, so it costs O(n) in the number
 * of card properties even when the answer is trivial. Do not call it in a loop
 * over a board's properties. Use visibleProperties for that, which resolves the
 * whole list against a single shared index and memo.
 *
 * The template passed in is used as-is for its own condition, so this also answers
 * the question for a template that is not on the board yet. The rest of the chain
 * is resolved through allTemplates.
 */
function isPropertyVisible(template: IPropertyTemplate, card: Card, allTemplates: readonly IPropertyTemplate[]): boolean {
    const context = createContext(allTemplates)
    context.templatesById.set(template.id, template)

    return resolveVisibility(context, new Map<string, boolean>(), card, template.id)
}

/**
 * The card properties that should be shown on this card, in board order.
 *
 * Card templates are a deliberate exception and always get the full list. A
 * template exists to carry default values, and it usually holds no value for a
 * condition source, so resolving it normally would hide exactly the properties
 * whose defaults the user opened the template to set. This is a display
 * short-circuit rather than a data rule, which is why it lives here and not in
 * isPropertyVisible: that stays a pure schema-plus-card primitive.
 */
function visibleProperties(card: Card, allTemplates: readonly IPropertyTemplate[]): IPropertyTemplate[] {
    if (card.fields.isTemplate) {
        return [...allTemplates]
    }

    const context = createContext(allTemplates)
    const memo = new Map<string, boolean>()

    return allTemplates.filter((template) => resolveVisibility(context, memo, card, template.id))
}

// Rotates a cycle so that it starts at its lowest-indexed member, keeping the
// traversal order intact. Without this the reported order would depend on which
// property the detector happened to enter the cycle from.
function rotateToLowestIndex(cycle: string[], indexById: Map<string, number>): string[] {
    let pivot = 0
    for (let i = 1; i < cycle.length; i++) {
        if (indexById.get(cycle[i])! < indexById.get(cycle[pivot])!) {
            pivot = i
        }
    }

    return [...cycle.slice(pivot), ...cycle.slice(0, pivot)]
}

/**
 * Every problem with the visibility conditions on a board, independent of any
 * card. Nothing on a render path needs to call this: the resolver already fails
 * open on all of these. It exists for the property editor, which should warn about
 * conditions that do nothing.
 *
 * Issues are sorted by the position of the property they belong to in
 * allTemplates, and cycles by their first (lowest-indexed) member, so the list is
 * stable across calls.
 */
function findVisibilityIssues(allTemplates: readonly IPropertyTemplate[]): VisibilityIssue[] {
    const context = createContext(allTemplates)
    const indexById = new Map<string, number>()
    allTemplates.forEach((template, index) => indexById.set(template.id, index))

    const conditionsById = new Map<string, ResolvedCondition>()
    const issues: VisibilityIssue[] = []

    // Broken references first, in board order.
    allTemplates.forEach((template) => {
        const condition = resolveCondition(context, template)
        conditionsById.set(template.id, condition)

        switch (condition.kind) {
        case 'missingSource': {
            issues.push({kind: 'missingSource', propertyId: template.id, sourcePropertyId: condition.sourcePropertyId})
            break
        }
        case 'invalidSourceType': {
            issues.push({kind: 'invalidSourceType', propertyId: template.id, sourcePropertyId: condition.source.id, sourceType: condition.source.type})
            break
        }
        case 'conditional': {
            // Reported whenever any referenced option is gone, even though the
            // condition still evaluates against the ones that remain.
            if (condition.missingOptionIds.length > 0) {
                issues.push({kind: 'missingOptions', propertyId: template.id, sourcePropertyId: condition.source.id, missingOptionIds: condition.missingOptionIds})
            }
            break
        }
        default: {
            break
        }
        }
    })

    // Then cycles. Each property is walked once, and a cycle is reported once
    // rather than once per member.
    const settled = new Set<string>()
    allTemplates.forEach((template) => {
        if (settled.has(template.id)) {
            return
        }

        const path: string[] = []
        const positions = new Map<string, number>()
        let cursor: string | undefined = template.id

        while (cursor !== undefined && !settled.has(cursor)) {
            const seenAt = positions.get(cursor)
            if (seenAt !== undefined) {
                issues.push({kind: 'cycle', propertyIds: rotateToLowestIndex(path.slice(seenAt), indexById)})
                break
            }

            positions.set(cursor, path.length)
            path.push(cursor)
            cursor = conditionEdge(conditionsById.get(cursor)!)
        }

        path.forEach((id) => settled.add(id))
    })

    // Array.prototype.sort is stable, so issues that share a position keep the
    // order they were pushed in: broken references before cycles.
    const positionOf = (issue: VisibilityIssue): number => (issue.kind === 'cycle' ? indexById.get(issue.propertyIds[0])! : indexById.get(issue.propertyId)!)

    return issues.sort((a, b) => positionOf(a) - positionOf(b))
}

export {
    VisibilityIssue,
    isPropertyVisible,
    visibleProperties,
    findVisibilityIssues,
}
