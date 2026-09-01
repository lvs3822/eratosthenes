// Copyright (c) 2015-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.
import React, {useMemo} from 'react'
import {useIntl, IntlShape} from 'react-intl'

import Menu from '../widgets/menu'
import propsRegistry from '../properties'
import {PropertyType} from '../properties/types'
import {IPropertyTemplate, IPropertyVisibility} from '../blocks/board'
import {Card} from '../blocks/card'
import {conditionSourceCandidates, visibleProperties} from '../propertyVisibility'
import './propertyMenu.scss'

type Props = {
    propertyId: string
    propertyName: string
    propertyType: PropertyType

    // Optional so that call sites which only rename, retype and delete keep
    // working unchanged. The visibility entry appears only when both are given.
    cardProperties?: IPropertyTemplate[]
    cards?: readonly Card[]

    onTypeAndNameChanged: (newType: PropertyType, newName: string) => void
    onDelete: (id: string) => void
    onVisibilityChanged?: (visibleWhen?: IPropertyVisibility) => void
}

function typeMenuTitle(intl: IntlShape, type: PropertyType): string {
    return `${intl.formatMessage({id: 'PropertyMenu.typeTitle', defaultMessage: 'Type'})}: ${type.displayName(intl)}`
}

type TypesProps = {
    label: string
    onTypeSelected: (type: PropertyType) => void
}

export const PropertyTypes = (props: TypesProps): JSX.Element => {
    const intl = useIntl()
    return (
        <>
            <Menu.Label>
                <b>{props.label}</b>
            </Menu.Label>

            <Menu.Separator/>

            {
                propsRegistry.list().map((p) => (
                    <Menu.Text
                        key={p.type}
                        id={p.type}
                        name={p.displayName(intl)}
                        onClick={() => props.onTypeSelected(p)}
                    />
                ))
            }
        </>
    )
}

// Adds or removes one option id, leaving the rest alone.
function toggleOptionId(optionIds: string[], optionId: string): string[] {
    return optionIds.includes(optionId) ? optionIds.filter((id) => id !== optionId) : [...optionIds, optionId]
}

// Cards that hold a value for this property and would stop showing it under the
// condition as currently configured. Informational only: hiding destroys nothing,
// the values come back as soon as the card matches again.
function countAffectedCards(cards: readonly Card[], template: IPropertyTemplate, cardProperties: IPropertyTemplate[], visibleWhen: IPropertyVisibility): number {
    // Resolve against the prospective schema, so the count reflects the condition
    // being configured rather than whatever was saved a moment ago.
    const prospective = cardProperties.map((o) => (o.id === template.id ? {...o, visibleWhen} : o))

    return cards.filter((card) => {
        const value = card.fields.properties[template.id]
        const hasValue = Array.isArray(value) ? value.length > 0 : Boolean(value)
        if (!hasValue) {
            return false
        }

        return !visibleProperties(card, prospective).some((o) => o.id === template.id)
    }).length
}

const PropertyMenu = (props: Props) => {
    const intl = useIntl()
    let currentPropertyName = props.propertyName

    const {cardProperties, cards, onVisibilityChanged} = props

    const template = cardProperties?.find((o) => o.id === props.propertyId)
    const visibleWhen = template?.visibleWhen
    const source = visibleWhen ? cardProperties?.find((o) => o.id === visibleWhen.propertyId) : undefined

    const candidates = useMemo(
        () => ((template && cardProperties) ? conditionSourceCandidates(template, cardProperties) : []),
        [template, cardProperties],
    )

    const affectedCards = useMemo(() => {
        if (!template || !cardProperties || !cards || !visibleWhen || visibleWhen.optionIds.length === 0) {
            return 0
        }

        return countAffectedCards(cards, template, cardProperties, visibleWhen)
    }, [cards, template, cardProperties, visibleWhen])

    const deleteText = intl.formatMessage({
        id: 'PropertyMenu.Delete',
        defaultMessage: 'Delete',
    })

    const visibilityTitle = source ? intl.formatMessage({
        id: 'PropertyMenu.visibilityWith',
        defaultMessage: 'Show only when: {propertyName}',
    }, {propertyName: source.name}) : intl.formatMessage({
        id: 'PropertyMenu.visibility',
        defaultMessage: 'Show only when…',
    })

    const canConfigureVisibility = Boolean(template && onVisibilityChanged)

    // Nothing on this board can serve as a condition, so the entry is present but
    // inert with the reason attached, rather than opening onto an empty list.
    const noCandidates = candidates.length === 0 && !visibleWhen

    // Menu wraps every child it is handed in a div, and React.Children.map invokes
    // its callback for null and boolean children too, so an inline {cond && ...}
    // that evaluates false still renders an empty wrapper div. Building the list
    // here keeps the menu free of stray divs when the visibility entry is absent,
    // and keeps those phantom divs from stealing hover and closing open submenus.
    const menuItems: JSX.Element[] = [
        <Menu.TextInput
            key='name'
            initialValue={props.propertyName}
            onConfirmValue={(n) => {
                props.onTypeAndNameChanged(props.propertyType, n)
                currentPropertyName = n
            }}
            onValueChanged={(n) => {
                currentPropertyName = n
            }}
        />,
        <Menu.SubMenu
            key='type'
            id='type'
            name={typeMenuTitle(intl, props.propertyType)}
        >
            <PropertyTypes
                label={intl.formatMessage({id: 'PropertyMenu.changeType', defaultMessage: 'Change property type'})}
                onTypeSelected={(type: PropertyType) => props.onTypeAndNameChanged(type, currentPropertyName)}
            />
        </Menu.SubMenu>,
    ]

    if (canConfigureVisibility && noCandidates) {
        // Present but inert, with the reason attached, rather than opening onto an
        // empty list.
        menuItems.push(
            <Menu.Text
                key='visibility'
                id='visibility'
                name={visibilityTitle}
                disabled={true}
                subText={intl.formatMessage({
                    id: 'PropertyMenu.noSourceProperties',
                    defaultMessage: 'No select property on this board can be used as a condition',
                })}
                onClick={() => undefined}
            />,
        )
    } else if (canConfigureVisibility) {
        menuItems.push(
            <Menu.SubMenu
                key='visibility'
                id='visibility'
                name={visibilityTitle}
            >
                <Menu.Switch
                    id='show-always'
                    name={intl.formatMessage({id: 'PropertyMenu.showAlways', defaultMessage: 'Show always'})}
                    isOn={!visibleWhen}
                    suppressItemClicked={true}
                    onClick={() => onVisibilityChanged!(undefined)}
                />
                <Menu.Separator/>
                {candidates.map((candidate) => (
                    <Menu.Switch
                        key={candidate.id}
                        id={candidate.id}
                        name={candidate.name}
                        isOn={visibleWhen?.propertyId === candidate.id}
                        suppressItemClicked={true}
                        onClick={(id) => onVisibilityChanged!(
                            visibleWhen?.propertyId === id ? undefined : {propertyId: id, optionIds: []},
                        )}
                    />
                ))}
                {source && visibleWhen &&
                    <>
                        <Menu.Separator/>
                        <Menu.Label>
                            {intl.formatMessage({id: 'PropertyMenu.sourceIs', defaultMessage: '{propertyName} is'}, {propertyName: source.name})}
                        </Menu.Label>
                        {source.options.map((option) => (
                            <Menu.Switch
                                key={option.id}
                                id={option.id}
                                name={option.value}
                                isOn={visibleWhen.optionIds.includes(option.id)}
                                suppressItemClicked={true}
                                onClick={(id) => onVisibilityChanged!({
                                    propertyId: visibleWhen.propertyId,
                                    optionIds: toggleOptionId(visibleWhen.optionIds, id),
                                })}
                            />
                        ))}
                        {affectedCards > 0 &&
                            <Menu.Label>
                                <span className='PropertyMenu__affectedCards'>
                                    {intl.formatMessage({
                                        id: 'PropertyMenu.hiddenValueCount',
                                        defaultMessage: '{count, plural, one {# card} other {# cards}} in this view have a value here',
                                    }, {count: affectedCards})}
                                </span>
                            </Menu.Label>
                        }
                    </>
                }
            </Menu.SubMenu>,
        )
    }

    menuItems.push(
        <Menu.Text
            key='delete'
            id='delete'
            name={deleteText}
            onClick={() => props.onDelete(props.propertyId)}
        />,
    )

    return <Menu>{menuItems}</Menu>
}

export default React.memo(PropertyMenu)
