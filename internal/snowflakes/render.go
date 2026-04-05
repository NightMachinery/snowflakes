package snowflakes

import (
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/a-h/templ"
)

type PageData struct {
	Title   string
	Token   string
	Room    *RoomView
	HasRoom bool
}

type RoomView struct {
	Code              string
	PublicURL         string
	Flash             string
	IsAdmin           bool
	ViewerToken       string
	ViewerName        string
	ViewerRole        string
	Participants      []ParticipantView
	Settings          SettingsView
	Packs             []WordPack
	PlayerCount       int
	ObserverCount     int
	Won               int
	CurrentCardNumber int
	RemainingCards    int
	Status            string
	GameFinished      bool
	CanStartGame      bool
	CanShowNextRound  bool
	Round             *RoundView
}

type ParticipantView struct {
	Token       string
	Name        string
	Role        string
	Admin       bool
	Creator     bool
	PendingRole string
	IsViewer    bool
}

type SettingsView struct {
	GuesserCount           int
	ClueSlotsMode          string
	FixedClueSlots         int
	GuessSubmissionMode    string
	GuessResolutionMode    string
	WordSelectionMode      string
	CardPoolSize           int
	ChoiceSlateSize        int
	ShowCardPoolToGuessers bool
	SelectedPack           string
}

type RoundView struct {
	Phase             string
	TargetWord        string
	CanSeeTarget      bool
	CanSeeChoiceWords bool
	CanSeeCardPool    bool
	CanManageRound    bool
	CardPool          []string
	ChoiceSlate       []ChoiceView
	EligibleCluegiver bool
	ActiveGuesser     bool
	Spokesperson      bool
	ClueSlots         int
	Clues             []ClueView
	VisibleClues      []string
	AllCluesSubmitted bool
	Guesses           []GuessView
	Result            string
	ActiveGuessers    []string
	CluegiverNames    []string
	RoundControllers  []string
}

type ChoiceView struct {
	Index         int
	DisplayNumber int
	Word          string
	Votes         int
	VotedByYou    bool
	Selected      bool
}

type ClueView struct {
	Key            string
	PlayerName     string
	Slot           int
	Text           string
	Invalid        bool
	Manual         bool
	Auto           bool
	Editable       bool
	SubmittedByYou bool
}

type GuessView struct {
	PlayerName string
	Guess      string
	Passed     bool
}

func (a *App) renderComponent(w http.ResponseWriter, r *http.Request, component templ.Component) {
	templ.Handler(component, templ.WithErrorHandler(func(_ *http.Request, err error) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		})
	})).ServeHTTP(w, r)
}

func (a *App) buildRoomView(room *Room, viewerToken string) RoomView {
	room.mu.RLock()
	defer room.mu.RUnlock()

	viewer := room.Participants[viewerToken]
	participants := make([]ParticipantView, 0, len(room.Participants))
	playerCount, observerCount := 0, 0
	for _, p := range room.Participants {
		pv := ParticipantView{Token: p.Token, Name: p.Name, Role: string(p.Role), Admin: p.Admin, Creator: p.Creator, IsViewer: p.Token == viewerToken}
		if p.PendingRole != nil {
			pv.PendingRole = string(*p.PendingRole)
		}
		if p.Role == RolePlayer {
			playerCount++
		} else {
			observerCount++
		}
		participants = append(participants, pv)
	}
	sort.Slice(participants, func(i, j int) bool {
		if participants[i].Creator != participants[j].Creator {
			return participants[i].Creator
		}
		if participants[i].Admin != participants[j].Admin {
			return participants[i].Admin
		}
		return strings.ToLower(participants[i].Name) < strings.ToLower(participants[j].Name)
	})

	view := RoomView{
		Code:         room.Code,
		PublicURL:    strings.TrimRight(a.cfg.PublicURL, "/") + "/rooms/" + room.Code,
		Flash:        room.flash,
		Participants: participants,
		Packs:        a.roomPacks(room),
		Settings: SettingsView{
			GuesserCount:           room.Settings.GuesserCount,
			ClueSlotsMode:          string(room.Settings.ClueSlotsMode),
			FixedClueSlots:         room.Settings.FixedClueSlots,
			GuessSubmissionMode:    string(room.Settings.GuessSubmissionMode),
			GuessResolutionMode:    string(room.Settings.GuessResolutionMode),
			WordSelectionMode:      string(normalizeWordSelectionMode(room.Settings.WordSelectionMode)),
			CardPoolSize:           room.Settings.CardPoolSize,
			ChoiceSlateSize:        room.Settings.ChoiceSlateSize,
			ShowCardPoolToGuessers: room.Settings.ShowCardPoolToGuessers,
			SelectedPack:           room.Settings.SelectedPack,
		},
		PlayerCount:   playerCount,
		ObserverCount: observerCount,
		Status:        string(room.Game.Status),
	}
	if viewer != nil {
		view.IsAdmin = viewer.Admin
		view.ViewerToken = viewer.Token
		view.ViewerName = viewer.Name
		view.ViewerRole = string(viewer.Role)
	}

	if room.Game != nil {
		view.Won = room.Game.Won
		view.CurrentCardNumber = min(room.Game.CurrentIndex+1, len(room.Game.Deck))
		view.RemainingCards = max(0, len(room.Game.Deck)-room.Game.CurrentIndex)
		view.GameFinished = room.Game.Status == GameFinished
		view.CanStartGame = room.Game.Status == GameLobby || room.Game.Status == GameFinished
		if room.Game.CurrentRound != nil {
			view.Round = room.roundViewLocked(viewerToken)
			view.CanShowNextRound = room.Game.Status == GameRunning && room.Game.CurrentRound.Phase == PhaseResolved && room.Game.CurrentIndex < len(room.Game.Deck)
		}
	}
	return view
}

func (r *Room) roundViewLocked(viewerToken string) *RoundView {
	round := r.round()
	if round == nil {
		return nil
	}
	guessers := r.activeGuessers(round)
	cluegivers := r.eligibleCluegivers(round)
	invalid := detectInvalidClues(round)
	voteCounts := map[int]int{}
	for _, idx := range round.VotesByToken {
		voteCounts[idx]++
	}

	canManageRound := r.canManageRound(round, viewerToken)
	isCluegiver := slicesContains(cluegivers, viewerToken)
	isActiveGuesser := slicesContains(guessers, viewerToken)
	canSeeChoiceWords := round.Phase == PhaseResolved || canManageRound || isCluegiver
	canSeeTarget := round.Phase == PhaseResolved
	if round.TargetWord != "" && (canManageRound || isCluegiver) {
		canSeeTarget = true
	}
	canSeeCardPool := canManageRound || isCluegiver || (r.Settings.ShowCardPoolToGuessers && isActiveGuesser && (round.Phase == PhaseGuessEntry || round.Phase == PhaseResolved))

	choices := make([]ChoiceView, 0, len(round.Card.Slate))
	votedIndex, voted := round.VotesByToken[viewerToken]
	for idx, word := range round.Card.Slate {
		choices = append(choices, ChoiceView{Index: idx, DisplayNumber: idx + 1, Word: word, Votes: voteCounts[idx], VotedByYou: voted && votedIndex == idx, Selected: round.TargetIndex == idx && round.TargetWord != ""})
	}

	clues := make([]ClueView, 0, len(round.Clues))
	validClues := make([]string, 0, len(round.Clues))
	for key, clue := range round.Clues {
		name := clue.PlayerToken
		if p := r.Participants[clue.PlayerToken]; p != nil {
			name = p.Name
		}
		cv := ClueView{Key: key, PlayerName: name, Slot: clue.Slot, Text: clue.Text, Invalid: invalid[key], Manual: round.ManualInvalid[key], Auto: invalid[key] && !round.ManualInvalid[key], Editable: canManageRound && round.Phase == PhaseClueReview, SubmittedByYou: clue.PlayerToken == viewerToken}
		clues = append(clues, cv)
		if !invalid[key] {
			validClues = append(validClues, clue.Text)
		}
	}
	sort.Slice(clues, func(i, j int) bool {
		if clues[i].PlayerName == clues[j].PlayerName {
			return clues[i].Slot < clues[j].Slot
		}
		return strings.ToLower(clues[i].PlayerName) < strings.ToLower(clues[j].PlayerName)
	})
	sort.Strings(validClues)
	guesses := make([]GuessView, 0, len(round.Guesses)+len(round.PassByToken))
	for _, token := range guessers {
		name := token
		if p := r.Participants[token]; p != nil {
			name = p.Name
		}
		guesses = append(guesses, GuessView{PlayerName: name, Guess: round.Guesses[token], Passed: round.PassByToken[token]})
	}
	activeNames := namesForTokens(r.Participants, guessers)
	cluegiverNames := namesForTokens(r.Participants, cluegivers)
	return &RoundView{
		Phase:             string(round.Phase),
		TargetWord:        round.TargetWord,
		CanSeeTarget:      canSeeTarget,
		CanSeeChoiceWords: canSeeChoiceWords,
		CanSeeCardPool:    canSeeCardPool,
		CanManageRound:    canManageRound,
		CardPool:          append([]string{}, round.Card.Pool...),
		ChoiceSlate:       choices,
		EligibleCluegiver: isCluegiver,
		ActiveGuesser:     isActiveGuesser,
		Spokesperson:      r.spokesperson(round) == viewerToken,
		ClueSlots:         r.effectiveClueSlots(round),
		Clues:             clues,
		VisibleClues:      validClues,
		AllCluesSubmitted: r.allCluesSubmitted(round),
		Guesses:           guesses,
		Result:            round.Result,
		ActiveGuessers:    activeNames,
		CluegiverNames:    cluegiverNames,
		RoundControllers:  namesForTokens(r.Participants, r.roundControllers(round)),
	}
}

func namesForTokens(participants map[string]*Participant, tokens []string) []string {
	out := make([]string, 0, len(tokens))
	for _, token := range tokens {
		if p := participants[token]; p != nil && strings.TrimSpace(p.Name) != "" {
			out = append(out, p.Name)
		} else {
			out = append(out, token)
		}
	}
	return out
}

func slicesContains(values []string, needle string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}

func itoa(value int) string {
	return strconv.Itoa(value)
}

func commaSeparated(values []string) string {
	return strings.Join(values, ", ")
}

func roomPath(code string) string {
	return "/rooms/" + code
}

func roomActionPath(code, action string) string {
	return roomPath(code) + "/actions/" + action
}

func roomPacksPath(code string) string {
	return roomPath(code) + "/packs/upload"
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func actionError(room *Room, err error) {
	if err != nil {
		room.setFlash(err.Error())
	}
}

func mustInt(formValue string, fallback int) int {
	if n, err := strconv.Atoi(strings.TrimSpace(formValue)); err == nil {
		return n
	}
	return fallback
}
