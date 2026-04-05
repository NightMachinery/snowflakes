package snowflakes

import (
	mrand "math/rand"
	"testing"
)

func TestParseWordPackTextDedupesAndTrims(t *testing.T) {
	pack, err := parseWordPackText("Example", " apple \n\nAPPLE\r\nbanana\n", "test")
	if err != nil {
		t.Fatalf("parseWordPackText returned error: %v", err)
	}
	if len(pack.Words) != 2 {
		t.Fatalf("expected 2 unique words, got %d: %#v", len(pack.Words), pack.Words)
	}
	if pack.Words[0] != "apple" || pack.Words[1] != "banana" {
		t.Fatalf("unexpected words: %#v", pack.Words)
	}
}

func TestBuildDeckRequiresEnoughWords(t *testing.T) {
	_, err := buildDeck(mrand.New(mrand.NewSource(1)), WordPack{Name: "tiny", Words: []string{"a", "b"}}, defaultRoomSettings())
	if err == nil {
		t.Fatal("expected error for undersized word pack")
	}
}

func TestDefaultRoomSettingsUsesAdminPick(t *testing.T) {
	settings := defaultRoomSettings()
	if settings.WordSelectionMode != SelectionAdminPick {
		t.Fatalf("expected default word selection mode %q, got %q", SelectionAdminPick, settings.WordSelectionMode)
	}
}

func TestDetectInvalidCluesMarksDuplicatesAndManualInvalid(t *testing.T) {
	round := &Round{
		Clues: map[string]ClueSubmission{
			"a:1": {PlayerToken: "a", Slot: 1, Text: " Apple "},
			"b:1": {PlayerToken: "b", Slot: 1, Text: "apple"},
			"c:1": {PlayerToken: "c", Slot: 1, Text: "pear"},
		},
		ManualInvalid: map[string]bool{"c:1": true},
	}
	invalid := detectInvalidClues(round)
	if !invalid["a:1"] || !invalid["b:1"] || !invalid["c:1"] {
		t.Fatalf("unexpected invalid map: %#v", invalid)
	}
}

func TestToggleManualInvalidRejectsDuplicateClues(t *testing.T) {
	room := newPermissionTestRoom()
	room.Game.CurrentRound.Phase = PhaseClueReview
	room.Game.CurrentRound.Clues = map[string]ClueSubmission{
		"b:1": {PlayerToken: "b", Slot: 1, Text: " Orchard "},
		"a:1": {PlayerToken: "a", Slot: 1, Text: "orchard"},
	}

	if err := room.toggleManualInvalid("b", "b:1"); err == nil {
		t.Fatal("expected duplicate clue toggle to fail")
	}
}

func TestResolveRoundWrongBurnsExtraCard(t *testing.T) {
	room := &Room{Game: &Game{Deck: make([]RoundCard, 13), CurrentRound: &Round{}}}
	if err := room.resolveRound("wrong"); err != nil {
		t.Fatalf("resolveRound returned error: %v", err)
	}
	if room.Game.CurrentIndex != 2 {
		t.Fatalf("expected current index 2, got %d", room.Game.CurrentIndex)
	}
}

func newPermissionTestRoom() *Room {
	room := &Room{
		Code: "ABCDE",
		Participants: map[string]*Participant{
			"a": {Token: "a", Name: "Alice", Role: RolePlayer, Admin: true, Creator: true},
			"b": {Token: "b", Name: "Bob", Role: RolePlayer},
		},
		Order:       []string{"a", "b"},
		Settings:    defaultRoomSettings(),
		CustomPacks: map[string]WordPack{},
		Game: &Game{
			Status:       GameRunning,
			CurrentIndex: 0,
			CurrentRound: &Round{
				Phase:         PhaseWordSelection,
				Card:          RoundCard{Pool: []string{"Apple", "Pear", "Peach"}, Slate: []string{"Apple", "Pear", "Peach"}},
				TargetIndex:   1,
				TargetWord:    "Pear",
				VotesByToken:  map[string]int{},
				Clues:         map[string]ClueSubmission{},
				ManualInvalid: map[string]bool{},
				Guesses:       map[string]string{},
				PassByToken:   map[string]bool{},
			},
		},
	}
	room.assignTemporaryRoundController(room.Game.CurrentRound)
	return room
}

func newPermissionTestApp() *App {
	return &App{
		cfg:   Config{PublicURL: "http://example.com"},
		rooms: map[string]*Room{},
		packs: map[string]WordPack{},
	}
}

func TestBuildRoomViewWordSelectionVisibilityByRole(t *testing.T) {
	app := newTestApp(t)
	room := newRoundTestRoom(t, app)
	withRoomLock(t, room, func(room *Room, round *Round) {
		room.Settings.WordSelectionMode = SelectionAdminPick
		round.Phase = PhaseWordSelection
		round.TargetWord = ""
	})

	aliceView := app.buildRoomView(room, aliceToken)
	if !aliceView.IsAdmin {
		t.Fatal("expected creator guesser to remain an admin")
	}
	if aliceView.Round == nil || !aliceView.Round.ActiveGuesser {
		t.Fatalf("expected active guesser round view, got %#v", aliceView.Round)
	}
	if aliceView.Round.CanManageRound || aliceView.Round.CanSeeChoiceWords || aliceView.Round.CanSeeTarget {
		t.Fatalf("expected guesser hidden-info protections, got %#v", aliceView.Round)
	}
	if len(aliceView.Round.RoundControllers) != 1 || aliceView.Round.RoundControllers[0] != "Bob" {
		t.Fatalf("expected Bob as temporary round controller, got %#v", aliceView.Round.RoundControllers)
	}

	bobView := app.buildRoomView(room, bobToken)
	if bobView.Round == nil || !bobView.Round.EligibleCluegiver || !bobView.Round.CanManageRound {
		t.Fatalf("expected Bob to be an eligible clue-giver and round controller, got %#v", bobView.Round)
	}
	if !bobView.Round.CanSeeChoiceWords {
		t.Fatal("expected round controller clue-giver to see choice words")
	}
	if bobView.Round.CanSeeTarget {
		t.Fatal("did not expect target word before selection")
	}

	daveView := app.buildRoomView(room, daveToken)
	if daveView.ViewerRole != string(RoleObserver) {
		t.Fatalf("expected observer viewer role, got %q", daveView.ViewerRole)
	}
	if daveView.Round == nil {
		t.Fatal("expected round view for observer")
	}
	if daveView.Round.EligibleCluegiver || daveView.Round.ActiveGuesser || daveView.Round.CanManageRound || daveView.Round.CanSeeChoiceWords || daveView.Round.CanSeeTarget {
		t.Fatalf("expected observer to stay outside hidden info and controls, got %#v", daveView.Round)
	}
}

func TestBuildRoomViewPlayerVoteShowsVotesAndSelections(t *testing.T) {
	app := newTestApp(t)
	room := newRoundTestRoom(t, app)
	withRoomLock(t, room, func(room *Room, round *Round) {
		room.Settings.WordSelectionMode = SelectionPlayerVote
		round.Phase = PhaseWordSelection
		round.VotesByToken[bobToken] = 1
		round.VotesByToken[caraToken] = 0
	})

	view := app.buildRoomView(room, bobToken)
	if view.Round == nil {
		t.Fatal("expected round view")
	}
	if !view.Round.EligibleCluegiver || !view.Round.CanManageRound || !view.Round.CanSeeChoiceWords {
		t.Fatalf("expected voting clue-giver controller view, got %#v", view.Round)
	}
	if len(view.Round.ChoiceSlate) != 3 {
		t.Fatalf("expected 3 visible choices, got %#v", view.Round.ChoiceSlate)
	}
	if !view.Round.ChoiceSlate[1].VotedByYou {
		t.Fatalf("expected Bob's voted choice to be marked, got %#v", view.Round.ChoiceSlate)
	}
	if view.Round.ChoiceSlate[1].Votes != 1 || view.Round.ChoiceSlate[0].Votes != 1 {
		t.Fatalf("expected aggregated vote counts, got %#v", view.Round.ChoiceSlate)
	}
}

func TestBuildRoomViewResolvedRoundRevealsHiddenInfoToGuessers(t *testing.T) {
	app := newTestApp(t)
	room := newRoundTestRoom(t, app)
	withRoomLock(t, room, func(room *Room, round *Round) {
		round.Phase = PhaseResolved
		round.TargetIndex = 1
		round.TargetWord = "Pear"
		round.Result = "correct"
		round.Clues[clueKey(bobToken, 1)] = ClueSubmission{PlayerToken: bobToken, Slot: 1, Text: "orchard"}
		round.Clues[clueKey(caraToken, 1)] = ClueSubmission{PlayerToken: caraToken, Slot: 1, Text: "green"}
		round.ManualInvalid[clueKey(caraToken, 1)] = true
		room.Settings.ShowCardPoolToGuessers = false
	})

	view := app.buildRoomView(room, aliceToken)
	if view.Round == nil {
		t.Fatal("expected round view")
	}
	if !view.Round.CanSeeTarget || !view.Round.CanSeeChoiceWords {
		t.Fatalf("expected resolved round to reveal target and slate, got %#v", view.Round)
	}
	if view.Round.CanSeeCardPool {
		t.Fatal("did not expect card pool visibility without the setting enabled")
	}
	if len(view.Round.VisibleClues) != 1 || view.Round.VisibleClues[0] != "orchard" {
		t.Fatalf("expected only valid clues to remain visible, got %#v", view.Round.VisibleClues)
	}
}

func TestBuildRoomViewGuessPhaseTracksGuessesAndSpokesperson(t *testing.T) {
	app := newTestApp(t)
	room := newRoundTestRoom(t, app)
	withRoomLock(t, room, func(room *Room, round *Round) {
		room.Settings.GuesserCount = 2
		room.Settings.GuessSubmissionMode = GuessModeOneEach
		round.Phase = PhaseGuessEntry
		round.TargetWord = "Pear"
		round.Clues[clueKey(caraToken, 1)] = ClueSubmission{PlayerToken: caraToken, Slot: 1, Text: "orchard"}
		round.Guesses[aliceToken] = "Peach"
		round.PassByToken[bobToken] = true
	})

	aliceView := app.buildRoomView(room, aliceToken)
	if aliceView.Round == nil || !aliceView.Round.ActiveGuesser || !aliceView.Round.Spokesperson {
		t.Fatalf("expected Alice to be the spokesperson guesser, got %#v", aliceView.Round)
	}
	if len(aliceView.Round.Guesses) != 2 {
		t.Fatalf("expected both guessers to be shown in the guess list, got %#v", aliceView.Round.Guesses)
	}
	if got := aliceView.Round.ActiveGuessers; len(got) != 2 || got[0] != "Alice" || got[1] != "Bob" {
		t.Fatalf("unexpected active guesser names: %#v", got)
	}

	bobView := app.buildRoomView(room, bobToken)
	if bobView.Round == nil || !bobView.Round.ActiveGuesser || bobView.Round.Spokesperson {
		t.Fatalf("expected Bob to be an active non-spokesperson guesser, got %#v", bobView.Round)
	}
}

func TestAdminGuesserLosesHiddenInfoAndRoundControls(t *testing.T) {
	room := newPermissionTestRoom()
	view := newPermissionTestApp().buildRoomView(room, "a")

	if !view.IsAdmin {
		t.Fatal("expected creator to remain a real admin")
	}
	if view.Round == nil || !view.Round.ActiveGuesser {
		t.Fatalf("expected round view with active guesser, got %#v", view.Round)
	}
	if view.Round.CanManageRound {
		t.Fatal("did not expect active admin guesser to manage the round")
	}
	if view.Round.CanSeeTarget {
		t.Fatal("did not expect active admin guesser to see target word")
	}
	if view.Round.CanSeeChoiceWords {
		t.Fatal("did not expect active admin guesser to see choice words")
	}
	if len(view.Round.RoundControllers) != 1 || view.Round.RoundControllers[0] != "Bob" {
		t.Fatalf("expected Bob to be the temporary round controller, got %#v", view.Round.RoundControllers)
	}
}

func TestTemporaryRoundControllerCanManageRound(t *testing.T) {
	room := newPermissionTestRoom()
	room.Settings.WordSelectionMode = SelectionAdminPick
	room.Game.CurrentRound.TargetIndex = 0
	room.Game.CurrentRound.TargetWord = ""

	if err := room.chooseWord("a", 0); err == nil {
		t.Fatal("expected active admin guesser to be blocked from choosing the round word")
	}
	if err := room.chooseWord("b", 0); err != nil {
		t.Fatalf("expected temporary round controller to choose the round word: %v", err)
	}
	if room.Game.CurrentRound.Phase != PhaseClueEntry {
		t.Fatalf("expected round to advance to clue entry, got %s", room.Game.CurrentRound.Phase)
	}
	if room.Game.CurrentRound.TargetWord != "Apple" {
		t.Fatalf("expected target word to be chosen, got %q", room.Game.CurrentRound.TargetWord)
	}
}

func TestAdminGuesserKeepsSafeAdminControls(t *testing.T) {
	room := newPermissionTestRoom()

	if err := room.setParticipantRole("a", "b", RoleObserver); err != nil {
		t.Fatalf("expected active admin guesser to retain safe admin controls: %v", err)
	}
	if room.Participants["b"].PendingRole == nil || *room.Participants["b"].PendingRole != RoleObserver {
		t.Fatalf("expected pending observer role for Bob, got %#v", room.Participants["b"].PendingRole)
	}
}

func TestNonGuessingAdminKeepsRoundControlsWithoutTemporaryAssignment(t *testing.T) {
	room := newPermissionTestRoom()
	room.Participants["b"].Admin = true
	room.assignTemporaryRoundController(room.Game.CurrentRound)

	if room.Game.CurrentRound.TemporaryRoundControllerToken != "" {
		t.Fatalf("did not expect temporary round controller when a non-guessing admin exists, got %q", room.Game.CurrentRound.TemporaryRoundControllerToken)
	}
	view := newPermissionTestApp().buildRoomView(room, "b")
	if view.Round == nil || !view.Round.CanManageRound {
		t.Fatalf("expected non-guessing admin to manage the round, got %#v", view.Round)
	}
}

func TestGuesserCardPoolOnlyVisibleDuringGuessPhaseWhenEnabled(t *testing.T) {
	room := newPermissionTestRoom()
	room.Settings.ShowCardPoolToGuessers = true

	view := newPermissionTestApp().buildRoomView(room, "a")
	if view.Round == nil {
		t.Fatal("expected round view")
	}
	if view.Round.CanSeeCardPool {
		t.Fatal("did not expect guesser to see answer bank during word selection")
	}

	room.Game.CurrentRound.Phase = PhaseGuessEntry
	view = newPermissionTestApp().buildRoomView(room, "a")
	if !view.Round.CanSeeCardPool {
		t.Fatal("expected guesser to see answer bank during guess phase")
	}
}

func TestSubmitCluesRequiresCompleteSet(t *testing.T) {
	room := newPermissionTestRoom()
	room.Game.CurrentRound.Phase = PhaseClueEntry
	room.Game.CurrentRound.TargetWord = "Pear"

	if err := room.submitClues("b", []string{"single"}); err == nil {
		t.Fatal("expected partial clue submission to fail")
	}
	if err := room.submitClues("b", []string{"orchard", "green"}); err != nil {
		t.Fatalf("expected complete clue submission to succeed: %v", err)
	}
	if got := len(room.Game.CurrentRound.Clues); got != 2 {
		t.Fatalf("expected 2 saved clues, got %d", got)
	}
	if !room.allCluesSubmitted(room.Game.CurrentRound) {
		t.Fatal("expected all clues to be submitted")
	}
	if room.Game.CurrentRound.Phase != PhaseClueReview {
		t.Fatalf("expected round to auto-advance to clue review, got %s", room.Game.CurrentRound.Phase)
	}
}

func TestSubmitCluesTrimsWhitespaceBeforeSaving(t *testing.T) {
	room := newPermissionTestRoom()
	room.Game.CurrentRound.Phase = PhaseClueEntry
	room.Game.CurrentRound.TargetWord = "Pear"

	if err := room.submitClues("b", []string{"  orchard  ", "\tgreen\n"}); err != nil {
		t.Fatalf("expected clue submission to succeed: %v", err)
	}
	if got := room.Game.CurrentRound.Clues["b:1"].Text; got != "orchard" {
		t.Fatalf("expected first clue to be trimmed, got %q", got)
	}
	if got := room.Game.CurrentRound.Clues["b:2"].Text; got != "green" {
		t.Fatalf("expected second clue to be trimmed, got %q", got)
	}
}

func TestSubmitGuessSpokespersonModeBlocksOtherGuessers(t *testing.T) {
	room := newRoundTestRoom(t, newTestApp(t))
	withRoomLock(t, room, func(room *Room, round *Round) {
		room.Settings.GuesserCount = 2
		room.Settings.GuessSubmissionMode = GuessModeSpokesperson
		round.Phase = PhaseGuessEntry
		round.TargetWord = "Pear"
	})

	if _, err := room.submitGuess(bobToken, "Pear", false); err == nil {
		t.Fatal("expected non-spokesperson guesser to be blocked")
	}
}

func TestSubmitGuessOneEachAllowsEachGuesser(t *testing.T) {
	room := newRoundTestRoom(t, newTestApp(t))
	withRoomLock(t, room, func(room *Room, round *Round) {
		room.Settings.GuesserCount = 2
		room.Settings.GuessSubmissionMode = GuessModeOneEach
		round.Phase = PhaseGuessEntry
		round.TargetWord = "Pear"
	})

	if resolved, err := room.submitGuess(bobToken, "Peach", false); err != nil || resolved {
		t.Fatalf("expected non-spokesperson guesser to submit a guess without auto-resolving, got resolved=%v err=%v", resolved, err)
	}
	if got := room.Game.CurrentRound.Guesses[bobToken]; got != "Peach" {
		t.Fatalf("expected Bob's guess to be stored, got %q", got)
	}
}

func TestSubmitGuessAutoExactResolvesCorrectGuess(t *testing.T) {
	room := newRoundTestRoom(t, newTestApp(t))
	withRoomLock(t, room, func(room *Room, round *Round) {
		room.Settings.GuessSubmissionMode = GuessModeSpokesperson
		room.Settings.GuessResolutionMode = GuessResolutionAutoExact
		round.Phase = PhaseGuessEntry
		round.TargetWord = "Pear"
	})

	resolved, err := room.submitGuess(aliceToken, " pear ", false)
	if err != nil {
		t.Fatalf("expected exact guess to succeed: %v", err)
	}
	if !resolved {
		t.Fatal("expected exact guess to resolve the round")
	}
	if room.Game.CurrentRound.Phase != PhaseResolved || room.Game.CurrentRound.Result != "correct" {
		t.Fatalf("expected resolved correct round, got %#v", room.Game.CurrentRound)
	}
}

func TestSubmitGuessPassResolvesForSpokespersonMode(t *testing.T) {
	room := newRoundTestRoom(t, newTestApp(t))
	withRoomLock(t, room, func(room *Room, round *Round) {
		room.Settings.GuessSubmissionMode = GuessModeSpokesperson
		round.Phase = PhaseGuessEntry
		round.TargetWord = "Pear"
	})

	resolved, err := room.submitGuess(aliceToken, "", true)
	if err != nil {
		t.Fatalf("expected spokesperson pass to succeed: %v", err)
	}
	if !resolved {
		t.Fatal("expected spokesperson pass to resolve the round")
	}
	if room.Game.CurrentRound.Result != "pass" {
		t.Fatalf("expected pass result, got %#v", room.Game.CurrentRound)
	}
}
