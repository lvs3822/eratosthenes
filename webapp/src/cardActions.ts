// Copyright (c) 2015-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

import {Card} from './blocks/card'
import mutator from './mutator'

// Deletes every card in the list as a single undo group, so one undo restores all of them.
// Takes plain data only: no React, no component props.
export async function deleteCardsInColumn(cards: Card[], description?: string): Promise<void> {
    if (cards.length === 0) {
        return
    }

    const actualDescription = description || (cards.length > 1 ? `delete ${cards.length} cards` : 'delete card')

    await mutator.performAsUndoGroup(async () => {
        await Promise.all(cards.map((card) => mutator.deleteBlock(card, actualDescription)))
    })
}
