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
			"a:1": {PlayerToken: "a", Slot: 1, Text: "Apple"},
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
