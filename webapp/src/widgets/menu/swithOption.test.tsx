// Copyright (c) 2015-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.
import React from 'react'
import {render, screen} from '@testing-library/react'
import '@testing-library/jest-dom'

import SwitchOption from './switchOption'

describe('widgets/menu/SwitchOption', () => {
    test('should call onClick and let the menu close when enabled', () => {
        const onClick = jest.fn()
        const dispatched = jest.fn()
        document.addEventListener('menuItemClicked', dispatched, true)

        const {container} = render(
            <SwitchOption
                id='test-id'
                name='Test'
                isOn={false}
                onClick={onClick}
            />,
        )

        const option = container.querySelector('.SwitchOption')
        option!.dispatchEvent(new MouseEvent('click', {bubbles: true}))

        expect(onClick).toHaveBeenCalledWith('test-id')
        expect(dispatched).toHaveBeenCalled()
        document.removeEventListener('menuItemClicked', dispatched, true)
    })

    test('should render a subtext and mark itself with the shared subtext class', () => {
        const {container} = render(
            <SwitchOption
                id='test-id'
                name='Test'
                isOn={true}
                subText='Why this is here'
                onClick={jest.fn()}
            />,
        )

        expect(screen.getByText('Why this is here')).toBeInTheDocument()
        expect(container.querySelector('.SwitchOption')).toHaveClass('menu-option--with-subtext')
        expect(container.querySelector('.menu-option__content')).toBeInTheDocument()
    })

    test('should render without the content wrapper when there is no subtext', () => {
        const {container} = render(
            <SwitchOption
                id='test-id'
                name='Test'
                isOn={false}
                onClick={jest.fn()}
            />,
        )

        expect(container.querySelector('.SwitchOption')).not.toHaveClass('menu-option--with-subtext')
        expect(container.querySelector('.menu-option__content')).toBeNull()
    })

    test('should neither act nor close the menu when disabled', () => {
        const onClick = jest.fn()
        const dispatched = jest.fn()
        document.addEventListener('menuItemClicked', dispatched, true)

        const {container} = render(
            <SwitchOption
                id='test-id'
                name='Test'
                isOn={true}
                disabled={true}
                onClick={onClick}
            />,
        )

        const option = container.querySelector('.SwitchOption')
        expect(option).toHaveClass('menu-option--disabled')
        option!.dispatchEvent(new MouseEvent('click', {bubbles: true}))

        expect(onClick).not.toHaveBeenCalled()
        expect(dispatched).not.toHaveBeenCalled()
        document.removeEventListener('menuItemClicked', dispatched, true)
    })

    test('should not dispatch menuItemClicked when suppressed', () => {
        const onClick = jest.fn()
        const dispatched = jest.fn()
        document.addEventListener('menuItemClicked', dispatched, true)

        const {container} = render(
            <SwitchOption
                id='test-id'
                name='Test'
                isOn={false}
                suppressItemClicked={true}
                onClick={onClick}
            />,
        )

        container.querySelector('.SwitchOption')!.dispatchEvent(new MouseEvent('click', {bubbles: true}))

        expect(onClick).toHaveBeenCalledWith('test-id')
        expect(dispatched).not.toHaveBeenCalled()
        document.removeEventListener('menuItemClicked', dispatched, true)
    })
})
