// Copyright (c) 2015-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

import {Block, createBlock} from './block'

type CardType = 'normal' | 'recurring'

type RecurrenceMode = 'schedule' | 'afterDone'

type RecurrenceHistoryMode = 'newInstance' | 'returnSame'

type RecurrenceRuleKind = 'daily' | 'weekly' | 'monthly'

// The period a schedule-mode recurrence repeats on.
type RecurrenceRule = {
    kind: RecurrenceRuleKind
    interval: number

    // Weekly rules only. ISO-8601 numbering: 1 is Monday and 7 is Sunday.
    weekdays?: number[]

    // Monthly rules only. A month too short for it uses its last day.
    dayOfMonth?: number
}

// The recurrence configuration of a card. Mirrors RecurrenceConfig in
// server/model/recurring_card.go; the two must be changed together.
type RecurrenceConfig = {
    enabled: boolean
    mode: RecurrenceMode

    groupPropertyId: string
    targetOptionId: string
    timezone: string

    // 'HH:MM' read in timezone. Applies to both modes.
    time: string
    historyMode: RecurrenceHistoryMode

    // Phase anchor in epoch milliseconds, set when the recurrence is enabled and
    // preserved across edits so that changing the rule does not shift the cycle.
    startAt: number

    // mode === 'schedule'
    rule?: RecurrenceRule

    // mode === 'afterDone'
    doneOptionId?: string
    delayDays?: number
}

type CardFields = {
    icon?: string
    isTemplate?: boolean
    properties: Record<string, string | string[]>
    contentOrder: Array<string | string[]>

    // Absent means 'normal'.
    cardType?: CardType
    recurrence?: RecurrenceConfig

    // Set on a card created by a recurring rule, holding the id of the card that
    // carries the rule. The occurrence has no rule of its own so that it cannot
    // recur, but the server needs the reference to find the rule when this card
    // reaches the done column.
    recurrenceSourceId?: string
}

type Card = Block & {
    fields: CardFields
}

function copyRecurrence(recurrence: RecurrenceConfig): RecurrenceConfig {
    return {
        ...recurrence,
        ...(recurrence.rule ? {
            rule: {
                ...recurrence.rule,
                ...(recurrence.rule.weekdays ? {weekdays: [...recurrence.rule.weekdays]} : {}),
            },
        } : {}),
    }
}

// isRecurrenceActive is the single predicate for "this card produces further
// occurrences". IsRecurrenceActive in server/model/recurring_card.go is the same
// one; do not spell the conjunction out anywhere else.
function isRecurrenceActive(card: Card): boolean {
    return card.fields.cardType === 'recurring' && Boolean(card.fields.recurrence?.enabled)
}

function createCard(block?: Block): Card {
    const contentOrder: Array<string|string[]> = []
    const contentIds = block?.fields?.contentOrder?.filter((id: any) => id !== null)

    if (contentIds?.length > 0) {
        for (const contentId of contentIds) {
            if (typeof contentId === 'string') {
                contentOrder.push(contentId)
            } else {
                contentOrder.push(contentId.slice())
            }
        }
    }
    return {
        ...createBlock(block),
        type: 'card',

        // NOTE: this is an allowlist. It copies the fields named below and
        // silently drops every other key the server sent. Any new entry in
        // CardFields MUST be copied here too, otherwise it is lost on the first
        // block update the client receives and then deleted on the next save,
        // because createPatchesFromBlocks puts keys missing from the new block
        // into deletedFields.
        fields: {
            icon: block?.fields.icon || '',
            properties: {...(block?.fields.properties || {})},
            contentOrder,
            isTemplate: block?.fields.isTemplate || false,

            // Emit these keys only when they are set, so that a card which is not
            // recurring keeps exactly the fields it had before this feature.
            ...(block?.fields.cardType ? {cardType: block.fields.cardType} : {}),
            ...(block?.fields.recurrence ? {recurrence: copyRecurrence(block.fields.recurrence)} : {}),
            ...(block?.fields.recurrenceSourceId ? {recurrenceSourceId: block.fields.recurrenceSourceId} : {}),
        },
    }
}

export {
    Card,
    CardType,
    RecurrenceConfig,
    RecurrenceRule,
    RecurrenceMode,
    RecurrenceHistoryMode,
    RecurrenceRuleKind,
    createCard,
    isRecurrenceActive,
}
