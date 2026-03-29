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
