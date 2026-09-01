// Copyright (c) 2015-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

import React from 'react'
import {fireEvent, render} from '@testing-library/react'
import '@testing-library/jest-dom'

import {wrapIntl} from '../testUtils'
import propsRegistry from '../properties'
import {IPropertyTemplate} from '../blocks/board'

import PropertyMenu from './propertyMenu'

describe('widgets/PropertyMenu', () => {
    beforeEach(() => {
        // Quick fix to disregard console error when unmounting a component
        console.error = jest.fn()
        document.execCommand = jest.fn()
    })

    test('should display the type of property', () => {
        const callback = jest.fn()
        const component = wrapIntl(
            <PropertyMenu
                propertyId={'id'}
                propertyName={'email of a person'}
                propertyType={propsRegistry.get('email')}
                onTypeAndNameChanged={callback}
                onDelete={callback}
            />,
        )
        const {getByText} = render(component)
        expect(getByText('Type: Email')).toBeVisible()
    })

    test('handles delete event', () => {
        const callback = jest.fn()
        const component = wrapIntl(
            <PropertyMenu
                propertyId={'id'}
                propertyName={'email of a person'}
                propertyType={propsRegistry.get('email')}
                onTypeAndNameChanged={callback}
                onDelete={callback}
            />,
        )
        const {getByText} = render(component)
        fireEvent.click(getByText(/delete/i))
        expect(callback).toHaveBeenCalledWith('id')
    })

    test('handles name change event', () => {
        const callback = jest.fn()
        const component = wrapIntl(
            <PropertyMenu
                propertyId={'id'}
                propertyName={'test-property'}
                propertyType={propsRegistry.get('text')}
                onTypeAndNameChanged={callback}
                onDelete={callback}
            />,
        )
        const {getByDisplayValue} = render(component)
        const input = getByDisplayValue(/test-property/i)
        fireEvent.change(input, {target: {value: 'changed name'}})
        fireEvent.blur(input)
        expect(callback).toHaveBeenCalledWith(propsRegistry.get('text'), 'changed name')
    })

    test('handles type change event', async () => {
        const callback = jest.fn()
        const component = wrapIntl(
            <PropertyMenu
                propertyId={'id'}
                propertyName={'test-property'}
                propertyType={propsRegistry.get('text')}
                onTypeAndNameChanged={callback}
                onDelete={callback}
            />,
        )
        const {getByText} = render(component)
        const menuOpen = getByText(/Type: Text/i)
        fireEvent.click(menuOpen)
        fireEvent.click(getByText('Select'))
        setTimeout(() => expect(callback).toHaveBeenCalledWith('select', 'test-property'), 2000)
    })

    test('handles name and type change event', () => {
        const callback = jest.fn()
        const component = wrapIntl(
            <PropertyMenu
                propertyId={'id'}
                propertyName={'test-property'}
                propertyType={propsRegistry.get('text')}
                onTypeAndNameChanged={callback}
                onDelete={callback}
            />,
        )
        const {getByDisplayValue, getByText} = render(component)
        const input = getByDisplayValue(/test-property/i)
        fireEvent.change(input, {target: {value: 'changed name'}})

        const menuOpen = getByText(/Type: Text/i)
        fireEvent.click(menuOpen)
        fireEvent.click(getByText('Select'))
        setTimeout(() => expect(callback).toHaveBeenCalledWith('select', 'changed name'), 2000)
    })

    test('should match snapshot', () => {
        const callback = jest.fn()
        const component = wrapIntl(
            <PropertyMenu
                propertyId={'id'}
                propertyName={'test-property'}
                propertyType={propsRegistry.get('text')}
                onTypeAndNameChanged={callback}
                onDelete={callback}
            />,
        )
        const {container, getByText} = render(component)
        const menuOpen = getByText(/Type: Text/i)
        fireEvent.click(menuOpen)
        expect(container).toMatchSnapshot()
    })

    describe('visibility conditions', () => {
        const status: IPropertyTemplate = {
            id: 'status_id',
            name: 'Status',
            type: 'select',
            options: [
                {id: 'status_todo', value: 'Todo', color: 'propColorDefault'},
                {id: 'status_blocked', value: 'Blocked', color: 'propColorDefault'},
            ],
        }
        const plainReason: IPropertyTemplate = {id: 'reason_id', name: 'Blocked reason', type: 'text', options: []}
        const conditionalReason: IPropertyTemplate = {
            ...plainReason,
            visibleWhen: {propertyId: 'status_id', optionIds: ['status_blocked']},
        }

        function renderMenu(cardProperties: IPropertyTemplate[], onVisibilityChanged: jest.Mock) {
            return render(wrapIntl(
                <PropertyMenu
                    propertyId={'reason_id'}
                    propertyName={'Blocked reason'}
                    propertyType={propsRegistry.get('text')}
                    cardProperties={cardProperties}
                    cards={[]}
                    onTypeAndNameChanged={jest.fn()}
                    onDelete={jest.fn()}
                    onVisibilityChanged={onVisibilityChanged}
                />,
            ))
        }

        test('should offer an eligible source property and set an empty condition when picked', () => {
            const onVisibilityChanged = jest.fn()
            const {getByText} = renderMenu([status, plainReason], onVisibilityChanged)

            fireEvent.mouseOver(getByText(/show only when/i))
            expect(getByText('Show always')).toBeVisible()

            fireEvent.click(getByText('Status'))

            // No options yet, which by the resolver's rule means "no constraint",
            // so nothing disappears mid-configuration.
            expect(onVisibilityChanged).toHaveBeenCalledWith({propertyId: 'status_id', optionIds: []})
        })

        test('should list the source options once a source is set, and toggle them', () => {
            const onVisibilityChanged = jest.fn()
            const {getByText} = renderMenu([status, conditionalReason], onVisibilityChanged)

            fireEvent.mouseOver(getByText(/show only when/i))
            expect(getByText('Status is')).toBeVisible()

            fireEvent.click(getByText('Todo'))
            expect(onVisibilityChanged).toHaveBeenCalledWith({propertyId: 'status_id', optionIds: ['status_blocked', 'status_todo']})

            fireEvent.click(getByText('Blocked'))
            expect(onVisibilityChanged).toHaveBeenCalledWith({propertyId: 'status_id', optionIds: []})
        })

        test('should clear the condition from Show always', () => {
            const onVisibilityChanged = jest.fn()
            const {getByText} = renderMenu([status, conditionalReason], onVisibilityChanged)

            fireEvent.mouseOver(getByText(/show only when/i))
            fireEvent.click(getByText('Show always'))

            expect(onVisibilityChanged).toHaveBeenCalledWith(undefined)
        })

        test('should not offer a property that would close a cycle', () => {
            // status is itself conditioned on reason, so offering it back would
            // make reason -> status -> reason.
            const cyclicStatus: IPropertyTemplate = {
                ...status,
                visibleWhen: {propertyId: 'reason_id', optionIds: []},
            }
            const {getByText, queryByText} = renderMenu([cyclicStatus, plainReason], jest.fn())

            fireEvent.mouseOver(getByText(/show only when/i))
            expect(queryByText('Status')).toBeNull()
        })

        test('should show the entry disabled when no property can serve as a condition', () => {
            const notes: IPropertyTemplate = {id: 'notes_id', name: 'Notes', type: 'text', options: []}
            const {getByText} = renderMenu([notes, plainReason], jest.fn())

            expect(getByText(/no select property/i)).toBeVisible()
        })

        test('should not show the entry at all without an onVisibilityChanged handler', () => {
            const {queryByText} = render(wrapIntl(
                <PropertyMenu
                    propertyId={'reason_id'}
                    propertyName={'Blocked reason'}
                    propertyType={propsRegistry.get('text')}
                    onTypeAndNameChanged={jest.fn()}
                    onDelete={jest.fn()}
                />,
            ))

            expect(queryByText(/show only when/i)).toBeNull()
        })
    })
})
