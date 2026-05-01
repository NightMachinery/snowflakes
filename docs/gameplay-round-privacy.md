# Gameplay visibility notes

_Date: 2026-05-01_

## Player-list round badges

During an active unresolved round, the people list can show two custom inline SVG badges:

- `guesser`: the participant is an active guesser for the current round.
- `needs clues`: the participant is an eligible clue-giver in clue-entry and has not submitted every required clue slot.

The `needs clues` badge is recalculated from the current clue slot count and disappears as soon as the player has submitted all slots, or when the round leaves clue entry.

## Clue review privacy

Submitted clue text stays hidden from active guessers until the controller reveals valid clues and the round enters the guess phase. This remains true even if a guesser is also present in the round controller token list.

During clue review:

- non-guessing round controllers and eligible clue-givers can review submitted clues;
- only non-guessing round controllers can reveal valid clues;
- active guessers and observers see a blind waiting state instead of the clue list.

## Observer changes during clue entry

If a clue-giver becomes an observer during clue entry, the round removes that player from the active clue-giver set and immediately rechecks whether every remaining clue-giver has submitted all required slots. If so, the round advances to clue review automatically.
