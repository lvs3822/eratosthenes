// Copyright (c) 2015-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

import {IntlShape} from 'react-intl'

import {Board, IPropertyOption} from './blocks/board'
import {Card, RecurrenceConfig, RecurrenceRule, isRecurrenceActive} from './blocks/card'

// 1 January 2024 was a Monday, so formatting the seven days from it yields the
// weekday names in the user's locale in ISO order, without seven translation keys.
// The settings form derives them the same way, so the two cannot drift apart.
const firstMonday = Date.UTC(2024, 0, 1)
const msPerDay = 86400000

const wholeWeek = [1, 2, 3, 4, 5, 6, 7]
const workingWeek = [1, 2, 3, 4, 5]

const defaultInterval = 1
const defaultDelayDays = 1
const defaultDayOfMonth = 1

function sameDays(weekdays: number[], expected: number[]): boolean {
    if (weekdays.length !== expected.length) {
        return false
    }

    const sorted = [...weekdays].sort((a, b) => a - b)

    return expected.every((day, index) => sorted[index] === day)
}

// "Mon, Wed, and Fri" in English, and whatever the locale's list conventions are
// elsewhere. Built with formatList rather than joined, so that languages which do
// not separate lists with commas still read correctly.
function weekdayNames(intl: IntlShape, weekdays: number[]): string {
    const sorted = [...weekdays].sort((a, b) => a - b)
    const names = sorted.map((weekday) => intl.formatDate(firstMonday + ((weekday - 1) * msPerDay), {weekday: 'short', timeZone: 'UTC'}))

    return intl.formatList(names, {type: 'conjunction'})
}

function weeklySummary(intl: IntlShape, rule: RecurrenceRule, time: string): string {
    const weekdays = rule.weekdays || []
    const interval = rule.interval || defaultInterval

    // Every day and every working day are worth saying plainly rather than listing
    // seven or five weekday names, and they are two of the most common rules.
    if (interval === defaultInterval && sameDays(weekdays, wholeWeek)) {
        return intl.formatMessage({
            id: 'RecurrenceSummary.everyDay',
            defaultMessage: 'Repeats every day at {time}',
        }, {time})
    }

    if (interval === defaultInterval && sameDays(weekdays, workingWeek)) {
        return intl.formatMessage({
            id: 'RecurrenceSummary.everyWeekday',
            defaultMessage: 'Repeats every weekday at {time}',
        }, {time})
    }

    // A weekly rule with no days chosen is incomplete rather than nonsensical: the
    // settings form can be in this state before a day is picked.
    if (weekdays.length === 0) {
        return intl.formatMessage({
            id: 'RecurrenceSummary.weeklyNoDays',
            defaultMessage: 'Repeats every {interval, plural, one {week} other {# weeks}} at {time}',
        }, {interval, time})
    }

    return intl.formatMessage({
        id: 'RecurrenceSummary.weekly',
        defaultMessage: 'Repeats every {interval, plural, one {week} other {# weeks}} on {days} at {time}',
    }, {interval, days: weekdayNames(intl, weekdays), time})
}

// recurrenceSummary puts a recurrence rule into one sentence.
//
// Every branch is a single complete message rather than assembled fragments, so a
// translator sees a whole sentence and can reorder it. That is why the cases below
// look repetitive: the repetition is what makes them translatable.
//
// This describes the RULE. It never states a date: what the rule will actually
// produce next is computed on the server, because the arithmetic behind it is
// subtle enough that a second implementation would eventually disagree.
function recurrenceSummary(intl: IntlShape, config: RecurrenceConfig, options: IPropertyOption[]): string {
    if (config.mode === 'afterDone') {
        const doneOption = options.find((option) => option.id === config.doneOptionId)

        return intl.formatMessage({
            id: 'RecurrenceSummary.afterDone',
            defaultMessage: 'Repeats {days, plural, one {# day} other {# days}} after reaching {column}, at {time}',
        }, {
            days: config.delayDays || defaultDelayDays,
            column: doneOption ? doneOption.value : intl.formatMessage({
                id: 'RecurrenceSummary.unknownColumn',
                defaultMessage: 'a column that no longer exists',
            }),
            time: config.time,
        })
    }

    const rule = config.rule
    if (!rule) {
        return intl.formatMessage({
            id: 'RecurrenceSummary.scheduleNoRule',
            defaultMessage: 'Repeats on a schedule',
        })
    }

    switch (rule.kind) {
    case 'weekly':
        return weeklySummary(intl, rule, config.time)
    case 'monthly':
        return intl.formatMessage({
            id: 'RecurrenceSummary.monthly',
            defaultMessage: 'Repeats every {interval, plural, one {month} other {# months}} on day {dayOfMonth} at {time}',
        }, {
            interval: rule.interval || defaultInterval,
            dayOfMonth: rule.dayOfMonth || defaultDayOfMonth,
            time: config.time,
        })
    default:
        return intl.formatMessage({
            id: 'RecurrenceSummary.daily',
            defaultMessage: 'Repeats every {interval, plural, one {day} other {# days}} at {time}',
        }, {interval: rule.interval || defaultInterval, time: config.time})
    }
}

// cardRecurrenceSummary describes a card's rule, or returns undefined when the card
// does not produce further occurrences of itself.
//
// The test is isRecurrenceActive, the same conjunction the scheduler uses, so that
// the badge means exactly what the server means. A card whose recurrence was turned
// off keeps its configuration, and would otherwise still be described as repeating.
function cardRecurrenceSummary(intl: IntlShape, card: Card, board: Board): string | undefined {
    const config = card.fields.recurrence
    if (!config || !isRecurrenceActive(card)) {
        return undefined
    }

    const groupProperty = board.cardProperties.find((property) => property.id === config.groupPropertyId)

    return recurrenceSummary(intl, config, groupProperty ? groupProperty.options : [])
}

export {recurrenceSummary, cardRecurrenceSummary}
