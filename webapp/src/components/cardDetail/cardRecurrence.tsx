// Copyright (c) 2015-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.
import React, {useCallback, useEffect, useMemo, useState} from 'react'
import {FormattedMessage, IntlShape, useIntl} from 'react-intl'

import {Board, IPropertyOption, IPropertyTemplate} from '../../blocks/board'
import {BoardView} from '../../blocks/boardView'
import {
    Card,
    CardType,
    RecurrenceConfig,
    RecurrenceHistoryMode,
    RecurrenceMode,
    RecurrencePreview,
    RecurrenceRuleKind,
} from '../../blocks/card'

import mutator from '../../mutator'
import octoClient from '../../octoClient'
import Switch from '../../widgets/switch'
import Button from '../../widgets/buttons/button'
import {Utils} from '../../utils'
import {sendFlashMessage} from '../flashMessages'
import {Permission} from '../../constants'
import {useHasCurrentBoardPermissions} from '../../hooks/permissions'

import './cardRecurrence.scss'

type Props = {
    board: Board
    card: Card
    activeView: BoardView
    readonly: boolean
}

// How long to wait after the last keystroke before asking the server what the
// configuration would do. The call is against a local server, so this is about not
// firing one request per character rather than about latency.
const previewDebounceMs = 400

const defaultTime = '09:00'
const defaultDelayDays = 1

// 1 January 2024 was a Monday, so formatting the seven days from it gives the
// weekday names in the user's locale without seven translation keys.
const firstMonday = Date.UTC(2024, 0, 1)

const weekdayNumbers = [1, 2, 3, 4, 5, 6, 7]

function selectProperties(board: Board): IPropertyTemplate[] {
    return board.cardProperties.filter((property) => property.type === 'select')
}

// The group-by property of the view the card was opened from, when that view has
// one and it is usable as a column set. groupById is optional: table, gallery and
// calendar views have none, and so does an ungrouped board view.
function defaultGroupProperty(board: Board, activeView: BoardView): IPropertyTemplate | undefined {
    const candidates = selectProperties(board)
    const fromView = candidates.find((property) => property.id === activeView.fields.groupById)

    return fromView || candidates[0]
}

function currentOptionId(card: Card, propertyId: string): string {
    const value = card.fields.properties[propertyId]

    return typeof value === 'string' ? value : ''
}

function defaultConfig(board: Board, card: Card, activeView: BoardView): RecurrenceConfig {
    const groupProperty = defaultGroupProperty(board, activeView)
    const options = groupProperty?.options || []

    // Where the card sits right now, rather than the first option: marking a card
    // recurring while it is in "To do" means "To do" is where it should come back,
    // and the first option could just as easily be the done column.
    const current = groupProperty ? currentOptionId(card, groupProperty.id) : ''
    const target = options.find((option) => option.id === current) || options[0]

    // Boards almost always order their statuses left to right towards completion,
    // so the last option is right far more often than the first. It is a guess the
    // user can see and change.
    const done = options[options.length - 1]

    return {
        enabled: true,
        mode: 'schedule',
        groupPropertyId: groupProperty?.id || '',
        targetOptionId: target?.id || '',
        timezone: new Intl.DateTimeFormat().resolvedOptions().timeZone,
        time: defaultTime,
        historyMode: 'newInstance',

        // The server owns the phase anchor: it sets it on the first enable and
        // preserves it through every later edit, so whatever is sent here is
        // discarded. Sending zero says so plainly.
        startAt: 0,

        rule: {kind: 'daily', interval: 1},
        doneOptionId: done?.id || '',
        delayDays: defaultDelayDays,
    }
}

// Server problems name the JSON path of the offending field. Turning that into
// something readable is worth the switch; the reason itself comes from the server
// and is already a sentence.
function fieldLabel(intl: IntlShape, field: string): string {
    switch (field) {
    case 'mode':
        return intl.formatMessage({id: 'CardRecurrence.field.mode', defaultMessage: 'Repeat'})
    case 'groupPropertyId':
        return intl.formatMessage({id: 'CardRecurrence.field.groupProperty', defaultMessage: 'Column property'})
    case 'targetOptionId':
        return intl.formatMessage({id: 'CardRecurrence.field.targetOption', defaultMessage: 'New card appears in'})
    case 'doneOptionId':
        return intl.formatMessage({id: 'CardRecurrence.field.doneOption', defaultMessage: 'Completed column'})
    case 'delayDays':
        return intl.formatMessage({id: 'CardRecurrence.field.delayDays', defaultMessage: 'Delay'})
    case 'timezone':
        return intl.formatMessage({id: 'CardRecurrence.field.timezone', defaultMessage: 'Time zone'})
    case 'time':
        return intl.formatMessage({id: 'CardRecurrence.field.time', defaultMessage: 'Time of day'})
    case 'historyMode':
        return intl.formatMessage({id: 'CardRecurrence.field.historyMode', defaultMessage: 'When completed'})
    case 'startAt':
        return intl.formatMessage({id: 'CardRecurrence.field.startAt', defaultMessage: 'Start'})
    case 'rule':
    case 'rule.kind':
        return intl.formatMessage({id: 'CardRecurrence.field.rule', defaultMessage: 'Schedule'})
    case 'rule.interval':
        return intl.formatMessage({id: 'CardRecurrence.field.interval', defaultMessage: 'Every'})
    case 'rule.weekdays':
        return intl.formatMessage({id: 'CardRecurrence.field.weekdays', defaultMessage: 'Days of the week'})
    case 'rule.dayOfMonth':
        return intl.formatMessage({id: 'CardRecurrence.field.dayOfMonth', defaultMessage: 'Day of the month'})
    default:
        return field
    }
}

const CardRecurrence = (props: Props) => {
    const {board, card, activeView, readonly} = props
    const intl = useIntl()
    const canEditBoardCards = useHasCurrentBoardPermissions([Permission.ManageBoardCards])

    const storedConfig = card.fields.recurrence
    const storedCardType: CardType = card.fields.cardType || 'normal'

    const [cardType, setCardType] = useState<CardType>(storedCardType)
    const [config, setConfig] = useState<RecurrenceConfig>(
        () => storedConfig || defaultConfig(board, card, activeView),
    )
    const [preview, setPreview] = useState<RecurrencePreview | undefined>(undefined)
    const [saving, setSaving] = useState(false)

    // What is currently stored, so the form can tell whether it has anything left
    // to save. Saving otherwise changes nothing on screen, since the form already
    // shows what was typed, and the button looks like it did nothing.
    const [savedConfig, setSavedConfig] = useState(() => JSON.stringify(storedConfig || null))

    const disabled = readonly || !canEditBoardCards
    const candidates = useMemo(() => selectProperties(board), [board.cardProperties])

    const groupProperty = board.cardProperties.find((property) => property.id === config.groupPropertyId)
    const options: IPropertyOption[] = groupProperty?.options || []

    const patch = useCallback((changes: Partial<RecurrenceConfig>) => {
        setConfig((current) => ({...current, ...changes}))
    }, [])

    const patchRule = useCallback((changes: Partial<NonNullable<RecurrenceConfig['rule']>>) => {
        setConfig((current) => ({
            ...current,
            rule: {kind: 'daily', interval: 1, ...current.rule, ...changes},
        }))
    }, [])

    // The date arithmetic lives on the server and only on the server: daylight
    // saving, short months and the phase anchor are subtle enough that a second
    // implementation here would agree with it until the day it did not. One request
    // returns both the next occurrence and every problem, so this drives the preview
    // line and the save button together.
    useEffect(() => {
        if (cardType !== 'recurring') {
            setPreview(undefined)
            return undefined
        }

        let cancelled = false
        const timer = setTimeout(async () => {
            try {
                const result = await octoClient.previewCardRecurrence(board.id, card.id, config)
                if (!cancelled) {
                    setPreview(result)
                }
            } catch (err: any) {
                Utils.logError(`Error previewing card recurrence: ${err?.toString()}`)
            }
        }, previewDebounceMs)

        return () => {
            cancelled = true
            clearTimeout(timer)
        }
    }, [cardType, config, board.id, card.id])

    const onCardTypeChanged = useCallback(async (newCardType: CardType) => {
        setCardType(newCardType)

        if (newCardType === 'normal' && storedCardType === 'recurring') {
            try {
                await mutator.deleteCardRecurrence(board.id, card)
            } catch (err: any) {
                Utils.logError(`Error stopping card recurrence: ${err?.toString()}`)
                sendFlashMessage({
                    content: intl.formatMessage({
                        id: 'CardRecurrence.stopFailed',
                        defaultMessage: 'Could not stop this card repeating. {reason}',
                    }, {reason: err?.message || ''}),
                    severity: 'high',
                })
                setCardType(storedCardType)
            }
        }
    }, [board.id, card, storedCardType, intl])

    const onSave = useCallback(async () => {
        setSaving(true)
        try {
            await mutator.setCardRecurrence(board.id, card, config)
            setSavedConfig(JSON.stringify(config))
        } catch (err: any) {
            Utils.logError(`Error saving card recurrence: ${err?.toString()}`)
            sendFlashMessage({
                content: intl.formatMessage({
                    id: 'CardRecurrence.saveFailed',
                    defaultMessage: 'Could not save the recurrence. {reason}',
                }, {reason: err?.message || ''}),
                severity: 'high',
            })
        } finally {
            setSaving(false)
        }
    }, [board.id, card, config, intl])

    // Nothing left to save once the form matches what is stored. This is the
    // success signal as well as the correct state: the button greys out.
    const unchanged = JSON.stringify(config) === savedConfig

    const toggleWeekday = useCallback((weekday: number) => {
        setConfig((current) => {
            const rule = current.rule || {kind: 'weekly' as RecurrenceRuleKind, interval: 1}
            const weekdays = rule.weekdays || []

            return {
                ...current,
                rule: {
                    ...rule,
                    weekdays: weekdays.includes(weekday) ? weekdays.filter((d) => d !== weekday) : [...weekdays, weekday].sort(),
                },
            }
        })
    }, [])

    // Two states, because a card that already reached the done column has a real
    // date pending and saying "3 days after it reaches Done" then would read as
    // "nothing is scheduled" at exactly the moment something is.
    function previewLine(): JSX.Element {
        if (preview && preview.nextRunAt) {
            const when = new Date(preview.nextRunAt)
            return (
                <FormattedMessage
                    id='CardRecurrence.nextOccurrence'
                    defaultMessage='Next occurrence: {when}'
                    values={{
                        when: intl.formatDate(when, {
                            timeZone: config.timezone,
                            weekday: 'long',
                            day: 'numeric',
                            month: 'long',
                            hour: '2-digit',
                            minute: '2-digit',
                        }),
                    }}
                />
            )
        }

        if (config.mode === 'afterDone') {
            const doneOption = options.find((option) => option.id === config.doneOptionId)
            return (
                <FormattedMessage
                    id='CardRecurrence.nextOccurrenceAfterDone'
                    defaultMessage='Next occurrence: {days, plural, one {# day} other {# days}} after this card reaches {column}'
                    values={{days: config.delayDays || defaultDelayDays, column: doneOption?.value || '…'}}
                />
            )
        }

        return (
            <FormattedMessage
                id='CardRecurrence.nextOccurrenceUnknown'
                defaultMessage='Next occurrence: not scheduled'
            />
        )
    }

    if (candidates.length === 0) {
        return (
            <div className='CardRecurrence'>
                <div className='octo-propertyrow'>
                    <div className='octo-propertyname'>
                        <FormattedMessage
                            id='CardRecurrence.cardType'
                            defaultMessage='Card type'
                        />
                    </div>
                    <div className='CardRecurrence__unavailable'>
                        <FormattedMessage
                            id='CardRecurrence.noSelectProperty'
                            defaultMessage='This board needs a select property before a card can repeat, because its options are the columns.'
                        />
                    </div>
                </div>
            </div>
        )
    }

    return (
        <div className='CardRecurrence'>
            <div className='octo-propertyrow'>
                <div className='octo-propertyname'>
                    <FormattedMessage
                        id='CardRecurrence.cardType'
                        defaultMessage='Card type'
                    />
                </div>
                <select
                    className='CardRecurrence__cardType'
                    value={cardType}
                    disabled={disabled}
                    onChange={(e) => onCardTypeChanged(e.target.value as CardType)}
                >
                    <option value='normal'>
                        {intl.formatMessage({id: 'CardRecurrence.typeNormal', defaultMessage: 'Normal'})}
                    </option>
                    <option value='recurring'>
                        {intl.formatMessage({id: 'CardRecurrence.typeRecurring', defaultMessage: 'Recurring'})}
                    </option>
                </select>
            </div>

            {/* Deleting a recurrence keeps its settings on the card, so say so
                whenever the question could arise rather than only at the moment of
                switching, when the user is not yet wondering. */}
            {cardType === 'normal' && storedConfig &&
                <div className='CardRecurrence__kept'>
                    <FormattedMessage
                        id='CardRecurrence.settingsKept'
                        defaultMessage='Recurrence settings are saved, and come back if you switch this card to Recurring again.'
                    />
                </div>}

            {cardType === 'recurring' &&
            <div className='CardRecurrence__settings'>
                <label className='CardRecurrence__row'>
                    <span className='CardRecurrence__label'>
                        <FormattedMessage
                            id='CardRecurrence.mode'
                            defaultMessage='Repeat'
                        />
                    </span>
                    <select
                        value={config.mode}
                        disabled={disabled}
                        onChange={(e) => patch({mode: e.target.value as RecurrenceMode})}
                    >
                        <option value='schedule'>
                            {intl.formatMessage({id: 'CardRecurrence.modeSchedule', defaultMessage: 'On a schedule'})}
                        </option>
                        <option value='afterDone'>
                            {intl.formatMessage({id: 'CardRecurrence.modeAfterDone', defaultMessage: 'After it reaches a column'})}
                        </option>
                    </select>
                </label>

                {config.mode === 'schedule' &&
                <>
                    <div className='CardRecurrence__row'>
                        <span className='CardRecurrence__label'>
                            <FormattedMessage
                                id='CardRecurrence.every'
                                defaultMessage='Every'
                            />
                        </span>
                        <input
                            type='number'
                            min='1'
                            className='CardRecurrence__interval'
                            value={config.rule?.interval ?? 1}
                            disabled={disabled}
                            onChange={(e) => patchRule({interval: parseInt(e.target.value, 10) || 1})}
                        />
                        <select
                            value={config.rule?.kind || 'daily'}
                            disabled={disabled}
                            onChange={(e) => patchRule({kind: e.target.value as RecurrenceRuleKind})}
                        >
                            <option value='daily'>
                                {intl.formatMessage({id: 'CardRecurrence.kindDaily', defaultMessage: 'days'})}
                            </option>
                            <option value='weekly'>
                                {intl.formatMessage({id: 'CardRecurrence.kindWeekly', defaultMessage: 'weeks'})}
                            </option>
                            <option value='monthly'>
                                {intl.formatMessage({id: 'CardRecurrence.kindMonthly', defaultMessage: 'months'})}
                            </option>
                        </select>
                    </div>

                    {config.rule?.kind === 'weekly' &&
                        <div className='CardRecurrence__row'>
                            <span className='CardRecurrence__label'>
                                <FormattedMessage
                                    id='CardRecurrence.weekdays'
                                    defaultMessage='On'
                                />
                            </span>
                            <div className='CardRecurrence__weekdays'>
                                {weekdayNumbers.map((weekday) => (
                                    <button
                                        key={weekday}
                                        type='button'
                                        disabled={disabled}
                                        className={(config.rule?.weekdays || []).includes(weekday) ? 'on' : ''}
                                        onClick={() => toggleWeekday(weekday)}
                                    >
                                        {intl.formatDate(firstMonday + ((weekday - 1) * 86400000), {weekday: 'short', timeZone: 'UTC'})}
                                    </button>
                                ))}
                            </div>
                        </div>}

                    {config.rule?.kind === 'monthly' &&
                        <label className='CardRecurrence__row'>
                            <span className='CardRecurrence__label'>
                                <FormattedMessage
                                    id='CardRecurrence.dayOfMonth'
                                    defaultMessage='On day'
                                />
                            </span>
                            <input
                                type='number'
                                min='1'
                                max='31'
                                className='CardRecurrence__interval'
                                value={config.rule?.dayOfMonth ?? 1}
                                disabled={disabled}
                                onChange={(e) => patchRule({dayOfMonth: parseInt(e.target.value, 10) || 1})}
                            />
                            <span className='CardRecurrence__hint'>
                                <FormattedMessage
                                    id='CardRecurrence.dayOfMonthHint'
                                    defaultMessage='A month that is too short uses its last day.'
                                />
                            </span>
                        </label>}
                </>}

                {config.mode === 'afterDone' &&
                <>
                    <label className='CardRecurrence__row'>
                        <span className='CardRecurrence__label'>
                            <FormattedMessage
                                id='CardRecurrence.doneColumn'
                                defaultMessage='Completed when it reaches'
                            />
                        </span>
                        <select
                            value={config.doneOptionId || ''}
                            disabled={disabled}
                            onChange={(e) => patch({doneOptionId: e.target.value})}
                        >
                            {options.map((option) => (
                                <option
                                    key={option.id}
                                    value={option.id}
                                >{option.value}</option>
                            ))}
                        </select>
                    </label>

                    <label className='CardRecurrence__row'>
                        <span className='CardRecurrence__label'>
                            <FormattedMessage
                                id='CardRecurrence.delayDays'
                                defaultMessage='Then come back after'
                            />
                        </span>
                        <input
                            type='number'
                            min='1'
                            className='CardRecurrence__interval'
                            value={config.delayDays ?? defaultDelayDays}
                            disabled={disabled}
                            onChange={(e) => patch({delayDays: parseInt(e.target.value, 10) || 1})}
                        />
                        <span className='CardRecurrence__hint'>
                            <FormattedMessage
                                id='CardRecurrence.delayDaysUnit'
                                defaultMessage='days'
                            />
                        </span>
                    </label>
                </>}

                <label className='CardRecurrence__row'>
                    <span className='CardRecurrence__label'>
                        <FormattedMessage
                            id='CardRecurrence.time'
                            defaultMessage='At'
                        />
                    </span>
                    <input
                        type='time'
                        value={config.time}
                        disabled={disabled}
                        onChange={(e) => patch({time: e.target.value})}
                    />
                    <span className='CardRecurrence__hint'>
                        <FormattedMessage
                            id='CardRecurrence.timezone'
                            defaultMessage='in {timezone}'
                            values={{timezone: config.timezone}}
                        />
                    </span>
                    <input
                        type='text'
                        className='CardRecurrence__timezone'
                        value={config.timezone}
                        disabled={disabled}
                        aria-label={intl.formatMessage({id: 'CardRecurrence.field.timezone', defaultMessage: 'Time zone'})}
                        onChange={(e) => patch({timezone: e.target.value})}
                    />
                </label>

                <label className='CardRecurrence__row'>
                    <span className='CardRecurrence__label'>
                        <FormattedMessage
                            id='CardRecurrence.groupProperty'
                            defaultMessage='Columns come from'
                        />
                    </span>
                    <select
                        value={config.groupPropertyId}
                        disabled={disabled}
                        onChange={(e) => patch({groupPropertyId: e.target.value, targetOptionId: '', doneOptionId: ''})}
                    >
                        {candidates.map((property) => (
                            <option
                                key={property.id}
                                value={property.id}
                            >{property.name}</option>
                        ))}
                    </select>
                </label>

                <label className='CardRecurrence__row'>
                    <span className='CardRecurrence__label'>
                        <FormattedMessage
                            id='CardRecurrence.targetColumn'
                            defaultMessage='New card appears in'
                        />
                    </span>
                    <select
                        value={config.targetOptionId || ''}
                        disabled={disabled}
                        onChange={(e) => patch({targetOptionId: e.target.value})}
                    >
                        {options.map((option) => (
                            <option
                                key={option.id}
                                value={option.id}
                            >{option.value}</option>
                        ))}
                    </select>
                </label>

                <label className='CardRecurrence__row'>
                    <span className='CardRecurrence__label'>
                        <FormattedMessage
                            id='CardRecurrence.historyMode'
                            defaultMessage='When completed'
                        />
                    </span>
                    <select
                        value={config.historyMode}
                        disabled={disabled}
                        onChange={(e) => patch({historyMode: e.target.value as RecurrenceHistoryMode})}
                    >
                        <option value='newInstance'>
                            {intl.formatMessage({id: 'CardRecurrence.historyNewInstance', defaultMessage: 'Create a new card (keeps a history)'})}
                        </option>
                        <option value='returnSame'>
                            {intl.formatMessage({id: 'CardRecurrence.historyReturnSame', defaultMessage: 'Move this one back (no history)'})}
                        </option>
                    </select>
                </label>

                <div className='CardRecurrence__row'>
                    <span className='CardRecurrence__label'>
                        <FormattedMessage
                            id='CardRecurrence.enabled'
                            defaultMessage='Enabled'
                        />
                    </span>
                    <Switch
                        isOn={config.enabled}
                        readOnly={disabled}
                        onChanged={(isOn) => patch({enabled: isOn})}
                    />
                </div>

                <div className='CardRecurrence__preview'>
                    {previewLine()}
                </div>

                {preview && preview.problems.length > 0 &&
                    <ul className='CardRecurrence__problems'>
                        {preview.problems.map((problem) => (
                            <li key={problem.field + problem.reason}>
                                <strong>{fieldLabel(intl, problem.field)}</strong>
                                {': '}
                                {problem.reason}
                            </li>
                        ))}
                    </ul>}

                <div className='CardRecurrence__actions'>
                    <Button
                        onClick={onSave}
                        filled={true}
                        disabled={disabled || saving || !preview || !preview.valid || unchanged}
                    >
                        <FormattedMessage
                            id='CardRecurrence.save'
                            defaultMessage='Save recurrence'
                        />
                    </Button>
                </div>
            </div>}
        </div>
    )
}

export default React.memo(CardRecurrence)
