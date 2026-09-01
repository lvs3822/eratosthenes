// Copyright (c) 2015-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.
import React from 'react'

import Switch from '../switch'

import {MenuOptionProps} from './menuItem'

type SwitchOptionProps = MenuOptionProps & {
    isOn: boolean
    icon?: React.ReactNode
    suppressItemClicked?: boolean

    // Both optional and undefined by default, mirroring TextOption's API and
    // reusing its class names so the styling is shared rather than duplicated.
    disabled?: boolean
    subText?: string
}

function SwitchOption(props: SwitchOptionProps): JSX.Element {
    const {name, icon, isOn, suppressItemClicked, disabled, subText} = props

    let className = 'MenuOption SwitchOption menu-option'
    if (subText) {
        className += ' menu-option--with-subtext'
    }
    if (disabled) {
        className += ' menu-option--disabled'
    }

    return (
        <div
            className={className}
            role='button'
            aria-label={name}
            onClick={(e: React.MouseEvent) => {
                if (disabled) {
                    // Neither act nor close the menu: a disabled option is inert.
                    e.stopPropagation()
                    return
                }
                if (!suppressItemClicked) {
                    e.target.dispatchEvent(new Event('menuItemClicked'))
                }
                props.onClick(props.id)
                e.stopPropagation()
            }}
        >
            {icon ? <div className='menu-option__icon'>{icon}</div> : <div className='noicon'/>}
            {subText ? (

                // Only wrapped when there is a subtext to wrap, so an option
                // without one renders exactly the DOM it always has.
                <div className='menu-option__content'>
                    <div className='menu-name'>{name}</div>
                    <div className='menu-subtext text-75 mt-1'>{subText}</div>
                </div>
            ) : (
                <div className='menu-name'>{name}</div>
            )}
            <Switch
                isOn={isOn}
                onChanged={() => {}}
            />
        </div>
    )
}

export default React.memo(SwitchOption)
