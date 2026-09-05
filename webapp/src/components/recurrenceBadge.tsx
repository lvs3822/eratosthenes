// Copyright (c) 2015-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.
import React from 'react'
import {useIntl} from 'react-intl'

import {Board} from '../blocks/board'
import {Card} from '../blocks/card'
import {cardRecurrenceSummary} from '../recurrenceSummary'
import CompassIcon from '../widgets/icons/compassIcon'

import './recurrenceBadge.scss'

type Props = {
    card: Card
    board: Board
}

// Marks a card that produces further occurrences of itself.
//
// Deliberately not part of CardBadges. Those count what is inside a card, are
// hidden by a per-view toggle, and disappear entirely when a card has no
// description, comments or checkboxes — which is exactly what a habit card looks
// like. Whether a card repeats is a structural fact about the card and has to be
// visible on all of them.
const RecurrenceBadge = (props: Props) => {
    const intl = useIntl()
    const summary = cardRecurrenceSummary(intl, props.card, props.board)

    if (!summary) {
        return null
    }

    return (
        <span
            className='RecurrenceBadge'
            title={summary}
        >
            <CompassIcon icon='sync'/>
        </span>
    )
}

export default React.memo(RecurrenceBadge)
