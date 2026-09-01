// Copyright (c) 2015-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

// A second view onto the same stored data as the property menu's "Show only
// when…" submenu. Both write through mutator.changePropertyVisibility, which is
// the only writer of IPropertyTemplate.visibleWhen, so there is no second
// storage format to keep in step.
//
// INVARIANT: this screen must not be able to construct any condition the
// property menu would refuse. Every property is checked against
// conditionSourceCandidates before it is offered as a checkbox; anything that
// fails lands in the read-only section with the reason attached. Without that,
// this menu would be a back door for exactly the cycles T3.4 prevents.

import React, {useMemo} from 'react'
import {useIntl} from 'react-intl'

import {Board, IPropertyTemplate} from '../../blocks/board'
import mutator from '../../mutator'
import {conditionSourceCandidates} from '../../propertyVisibility'
import {Utils} from '../../utils'
import Menu from '../../widgets/menu'

type Props = {
    board: Board
    groupByProperty?: IPropertyTemplate
    optionId: string
}

// Why a property cannot be toggled from this column.
type LockReason =
    | {kind: 'otherSource', sourceName: string}
    | {kind: 'self'}
    | {kind: 'wouldCycle'}

type Buckets = {
    scoped: IPropertyTemplate[]
    always: IPropertyTemplate[]
    locked: Array<{template: IPropertyTemplate, reason: LockReason}>
}

// Only a select property has the option columns this screen edits. multiSelect
// is a legal condition source but can never be grouped by, so it is unreachable
// here and gets no branch.
function isEligibleGroupBy(groupByProperty?: IPropertyTemplate): boolean {
    return groupByProperty?.type === 'select'
}

function bucketProperties(board: Board, groupByProperty: IPropertyTemplate): Buckets {
    const buckets: Buckets = {scoped: [], always: [], locked: []}

    board.cardProperties.forEach((template) => {
        if (template.id === groupByProperty.id) {
            buckets.locked.push({template, reason: {kind: 'self'}})
            return
        }

        const {visibleWhen} = template
        if (visibleWhen && visibleWhen.propertyId !== groupByProperty.id) {
            const source = board.cardProperties.find((o) => o.id === visibleWhen.propertyId)
            buckets.locked.push({template, reason: {kind: 'otherSource', sourceName: source ? source.name : visibleWhen.propertyId}})
            return
        }

        // The property menu's own rule, applied from this side.
        if (!conditionSourceCandidates(template, board.cardProperties).some((o) => o.id === groupByProperty.id)) {
            buckets.locked.push({template, reason: {kind: 'wouldCycle'}})
            return
        }

        // An empty optionIds means "no constraint" by the resolver's rule, so the
        // property is visible in every column and belongs with the others that
        // are. Checking it here would be the same all-or-nothing jump the
        // read-only section exists to prevent.
        if (visibleWhen && visibleWhen.optionIds.length > 0) {
            buckets.scoped.push(template)
        } else {
            buckets.always.push(template)
        }
    })

    return buckets
}

const ColumnPropertiesMenu = (props: Props): JSX.Element => {
    const {board, groupByProperty, optionId} = props
    const intl = useIntl()

    const entryTitle = intl.formatMessage({
        id: 'BoardComponent.column-properties',
        defaultMessage: 'Properties for this column',
    })

    const eligible = isEligibleGroupBy(groupByProperty)

    const buckets = useMemo(
        () => (eligible ? bucketProperties(board, groupByProperty!) : {scoped: [], always: [], locked: []}),
        [board.cardProperties, groupByProperty, eligible],
    )

    if (!eligible) {
        return (
            <Menu.Text
                id='columnProperties'
                name={entryTitle}
                disabled={true}
                subText={intl.formatMessage({
                    id: 'BoardComponent.column-properties-unavailable',
                    defaultMessage: 'Available when the board is grouped by a Select property',
                })}
                onClick={() => undefined}
            />
        )
    }

    const onToggle = async (template: IPropertyTemplate, checked: boolean) => {
        const optionIds = template.visibleWhen ? template.visibleWhen.optionIds : []
        const next = checked ? [...optionIds, optionId] : optionIds.filter((id) => id !== optionId)

        try {
            await mutator.changePropertyVisibility(board.id, board.cardProperties, template, {
                propertyId: groupByProperty!.id,
                optionIds: next,
            })
        } catch (err: any) {
            Utils.logError(`Error changing property visibility: ${template.name}: ${err?.toString()}`)
        }
    }

    const lockReasonText = (reason: LockReason): string => {
        switch (reason.kind) {
        case 'otherSource':
            return intl.formatMessage({
                id: 'BoardComponent.column-properties-other-source',
                defaultMessage: 'Conditioned on {propertyName}',
            }, {propertyName: reason.sourceName})
        case 'self':
            return intl.formatMessage({
                id: 'BoardComponent.column-properties-self',
                defaultMessage: "A property can't depend on itself",
            })
        default:
            return intl.formatMessage({
                id: 'BoardComponent.column-properties-would-cycle',
                defaultMessage: '{propertyName} already depends on this property',
            }, {propertyName: groupByProperty!.name})
        }
    }

    // Menu wraps every child in a div and React.Children.map fires for falsy
    // children too, so the list is built here rather than with inline
    // conditionals. SubMenuOption renders its own children raw, but keeping one
    // shape across the file is cheaper to reason about.
    const items: JSX.Element[] = []

    if (buckets.scoped.length > 0) {
        items.push(
            <Menu.Label key='scoped-header'>
                {intl.formatMessage({id: 'BoardComponent.column-properties-scoped', defaultMessage: 'Shown only in some columns'})}
            </Menu.Label>,
        )
        buckets.scoped.forEach((template) => {
            const optionIds = template.visibleWhen!.optionIds
            const checked = optionIds.includes(optionId)

            // Unchecking the last column would leave optionIds empty, which means
            // "no constraint", so the property would jump to the read-only section
            // and could never be brought back from here.
            const isOnlyColumn = checked && optionIds.length === 1

            items.push(
                <Menu.Switch
                    key={template.id}
                    id={template.id}
                    name={template.name}
                    isOn={checked}
                    disabled={isOnlyColumn}
                    subText={isOnlyColumn ? intl.formatMessage({
                        id: 'BoardComponent.column-properties-only-column',
                        defaultMessage: "This is the only column showing this property. Remove the condition from the property's menu on a card.",
                    }) : undefined}
                    suppressItemClicked={true}
                    onClick={() => onToggle(template, !checked)}
                />,
            )
        })
    }

    if (buckets.always.length > 0) {
        items.push(
            <Menu.Label key='always-header'>
                {intl.formatMessage({id: 'BoardComponent.column-properties-always', defaultMessage: 'Always visible'})}
            </Menu.Label>,
        )
        buckets.always.forEach((template) => {
            items.push(<Menu.Label key={template.id}>{template.name}</Menu.Label>)
        })
    }

    if (buckets.locked.length > 0) {
        items.push(
            <Menu.Label key='locked-header'>
                {intl.formatMessage({id: 'BoardComponent.column-properties-locked', defaultMessage: 'Not editable here'})}
            </Menu.Label>,
        )
        buckets.locked.forEach(({template, reason}) => {
            items.push(
                <Menu.Text
                    key={template.id}
                    id={template.id}
                    name={template.name}
                    disabled={true}
                    subText={lockReasonText(reason)}
                    onClick={() => undefined}
                />,
            )
        })
    }

    return (
        <Menu.SubMenu
            id='columnProperties'
            name={entryTitle}
        >
            {items}
        </Menu.SubMenu>
    )
}

export default React.memo(ColumnPropertiesMenu)
