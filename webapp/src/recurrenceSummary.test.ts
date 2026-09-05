// Copyright (c) 2015-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

import {createIntl} from 'react-intl'

import {IPropertyOption} from './blocks/board'
import {RecurrenceConfig} from './blocks/card'
import {recurrenceSummary} from './recurrenceSummary'

const intl = createIntl({locale: 'en'})

const options: IPropertyOption[] = [
    {id: 'opt-todo', value: 'To do', color: 'propColorDefault'},
    {id: 'opt-done', value: 'Done', color: 'propColorGreen'},
]

function schedule(overrides: Partial<RecurrenceConfig> = {}): RecurrenceConfig {
    return {
        enabled: true,
        mode: 'schedule',
        groupPropertyId: 'status-id',
        targetOptionId: 'opt-todo',
        timezone: 'Europe/Moscow',
        time: '09:00',
        historyMode: 'newInstance',
        startAt: 1767225600000,
        rule: {kind: 'daily', interval: 1},
        ...overrides,
    }
}

describe('recurrenceSummary', () => {
    describe('schedule mode', () => {
        it('should describe a daily rule in the singular', () => {
            expect(recurrenceSummary(intl, schedule(), options)).toBe('Repeats every day at 09:00')
        })

        it('should describe a multi-day interval in the plural', () => {
            const config = schedule({rule: {kind: 'daily', interval: 3}})
            expect(recurrenceSummary(intl, config, options)).toBe('Repeats every 3 days at 09:00')
        })

        it('should say "every day" rather than listing all seven weekdays', () => {
            const config = schedule({rule: {kind: 'weekly', interval: 1, weekdays: [1, 2, 3, 4, 5, 6, 7]}})
            expect(recurrenceSummary(intl, config, options)).toBe('Repeats every day at 09:00')
        })

        it('should say "every weekday" for Monday to Friday', () => {
            const config = schedule({rule: {kind: 'weekly', interval: 1, weekdays: [1, 2, 3, 4, 5]}})
            expect(recurrenceSummary(intl, config, options)).toBe('Repeats every weekday at 09:00')
        })

        it('should not say "every weekday" once the interval is more than one', () => {
            // Every other Monday to Friday is a different rule, and calling it
            // "every weekday" would be plainly wrong.
            const config = schedule({rule: {kind: 'weekly', interval: 2, weekdays: [1, 2, 3, 4, 5]}})
            expect(recurrenceSummary(intl, config, options)).toBe('Repeats every 2 weeks on Mon, Tue, Wed, Thu, and Fri at 09:00')
        })

        it('should list several weekdays in order, whatever order they were chosen in', () => {
            const config = schedule({rule: {kind: 'weekly', interval: 1, weekdays: [5, 1, 3]}})
            expect(recurrenceSummary(intl, config, options)).toBe('Repeats every week on Mon, Wed, and Fri at 09:00')
        })

        it('should describe a single weekday without a list separator', () => {
            const config = schedule({rule: {kind: 'weekly', interval: 1, weekdays: [7]}})
            expect(recurrenceSummary(intl, config, options)).toBe('Repeats every week on Sun at 09:00')
        })

        it('should describe a weekly rule with no days chosen yet', () => {
            const config = schedule({rule: {kind: 'weekly', interval: 2, weekdays: []}})
            expect(recurrenceSummary(intl, config, options)).toBe('Repeats every 2 weeks at 09:00')
        })

        it('should describe a monthly rule', () => {
            const config = schedule({rule: {kind: 'monthly', interval: 1, dayOfMonth: 15}})
            expect(recurrenceSummary(intl, config, options)).toBe('Repeats every month on day 15 at 09:00')
        })

        it('should describe a multi-month interval in the plural', () => {
            const config = schedule({rule: {kind: 'monthly', interval: 3, dayOfMonth: 31}})
            expect(recurrenceSummary(intl, config, options)).toBe('Repeats every 3 months on day 31 at 09:00')
        })

        it('should not fall over on a schedule with no rule', () => {
            const config = schedule({rule: undefined})
            expect(recurrenceSummary(intl, config, options)).toBe('Repeats on a schedule')
        })
    })

    describe('afterDone mode', () => {
        function afterDone(overrides: Partial<RecurrenceConfig> = {}): RecurrenceConfig {
            return schedule({
                mode: 'afterDone',
                rule: undefined,
                doneOptionId: 'opt-done',
                delayDays: 3,
                ...overrides,
            })
        }

        it('should name the column and the delay', () => {
            expect(recurrenceSummary(intl, afterDone(), options)).toBe('Repeats 3 days after reaching Done, at 09:00')
        })

        it('should use the singular for a delay of one day', () => {
            expect(recurrenceSummary(intl, afterDone({delayDays: 1}), options)).toBe('Repeats 1 day after reaching Done, at 09:00')
        })

        it('should say so when the done column has been deleted from the board', () => {
            // The rule outlives the option it points at, and a summary reading
            // "after reaching undefined" would be worse than saying nothing useful.
            const config = afterDone({doneOptionId: 'opt-that-was-deleted'})
            expect(recurrenceSummary(intl, config, options)).toBe('Repeats 3 days after reaching a column that no longer exists, at 09:00')
        })

        it('should ignore a rule left over from schedule mode', () => {
            const config = afterDone({rule: {kind: 'weekly', interval: 4, weekdays: [2]}})
            expect(recurrenceSummary(intl, config, options)).toBe('Repeats 3 days after reaching Done, at 09:00')
        })
    })
})
