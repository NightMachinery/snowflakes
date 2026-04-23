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
	GamePaused        bool
	GameFinished      bool
	CanStartGame      bool
	CanShowNextRound  bool
	Round             *RoundView
}

type ParticipantView struct {
	Token           string
	Name            string
	Role            string
	Admin           bool
	Creator         bool
	PendingRole     string
	IsViewer        bool
	RoundController bool
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
	ClueEntries       []ClueEntryFieldView
	YourClueCount     int
	TotalCluegivers   int
	SubmittedPlayers  int
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

type ClueEntryFieldView struct {
	Slot      int
	Text      string
	Submitted bool
}

type ClueView struct {
	Key            string
	PlayerName     string
	Slot           int
	Text           string
	Invalid        bool
	Duplicate      bool
	Manual         bool
	Auto           bool
	Editable       bool
	Toggleable     bool
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
	roundControllerTokens := map[string]bool{}
	if round := room.round(); round != nil {
		for _, token := range room.roundControllers(round) {
			roundControllerTokens[token] = true
		}
	}
	for _, p := range room.Participants {
		pv := ParticipantView{Token: p.Token, Name: p.Name, Role: string(p.Role), Admin: p.Admin, Creator: p.Creator, IsViewer: p.Token == viewerToken, RoundController: roundControllerTokens[p.Token]}
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
		view.GamePaused = room.Game.Status == GamePaused
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
	duplicates := detectDuplicateClues(round)
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
	clueEntries := make([]ClueEntryFieldView, 0, r.effectiveClueSlots(round))
	yourClueCount := 0
	for slot := 1; slot <= r.effectiveClueSlots(round); slot++ {
		text := ""
		submitted := false
		if clue, ok := round.Clues[clueKey(viewerToken, slot)]; ok {
			text = clue.Text
			submitted = true
			yourClueCount++
		}
		clueEntries = append(clueEntries, ClueEntryFieldView{Slot: slot, Text: text, Submitted: submitted})
	}
	for key, clue := range round.Clues {
		name := clue.PlayerToken
		if p := r.Participants[clue.PlayerToken]; p != nil {
			name = p.Name
		}
		cv := ClueView{
			Key:            key,
			PlayerName:     name,
			Slot:           clue.Slot,
			Text:           clue.Text,
			Invalid:        invalid[key],
			Duplicate:      duplicates[key],
			Manual:         round.ManualInvalid[key],
			Auto:           invalid[key] && !round.ManualInvalid[key],
			Editable:       canManageRound && round.Phase == PhaseClueReview,
			Toggleable:     canManageRound && round.Phase == PhaseClueReview && !duplicates[key],
			SubmittedByYou: clue.PlayerToken == viewerToken,
		}
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
		ClueEntries:       clueEntries,
		YourClueCount:     yourClueCount,
		TotalCluegivers:   len(cluegivers),
		SubmittedPlayers:  r.submittedCluegiverCount(round),
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

func pageClass(data PageData) string {
	if data.HasRoom && data.Room != nil {
		return "page-body page-body--room"
	}
	return "page-body page-body--landing"
}

func stageTitle(view RoomView) string {
	if view.Round == nil {
		if view.GamePaused {
			return "Game paused"
		}
		return "Ready to start"
	}
	switch view.Round.Phase {
	case string(PhaseWordSelection):
		return "Choose the target word"
	case string(PhaseClueEntry):
		return "Collect clues"
	case string(PhaseClueReview):
		return "Review invalid clues"
	case string(PhaseGuessEntry):
		return "Make the guess"
	default:
		return "Round resolved"
	}
}

func stageContext(view RoomView) string {
	if view.Round == nil {
		if view.GamePaused {
			return "The room will restart the game flow as soon as enough players are available again."
		}
		return "Choose a word, collect clues, review conflicts, and send the guessers in only when the hidden info is safe."
	}
	switch view.Round.Phase {
	case string(PhaseWordSelection):
		return "Only the right roles see the word controls, so the table can move fast without leaking the answer."
	case string(PhaseClueEntry):
		return "Clue-givers stay focused on their submissions while guessers remain deliberately blind."
	case string(PhaseClueReview):
		return "Duplicate clues and manual invalidations are surfaced together before anything is revealed."
	case string(PhaseGuessEntry):
		return "Valid clues take center stage while guessers and controllers act from the same control surface."
	default:
		return "The round result, valid clues, and next-step controls all land in one clear finish state."
	}
}

func displayEnum(value string) string {
	return strings.ReplaceAll(strings.TrimSpace(value), "_", " ")
}

func statusBadgeClass(value string) string {
	switch value {
	case string(GameRunning):
		return "status-chip status-chip--emerald"
	case string(GamePaused):
		return "status-chip status-chip--gold"
	case string(GameFinished):
		return "status-chip status-chip--violet"
	default:
		return "status-chip status-chip--ice"
	}
}

func phaseBadgeClass(value string) string {
	switch value {
	case string(PhaseWordSelection):
		return "status-chip status-chip--violet"
	case string(PhaseClueEntry):
		return "status-chip status-chip--cyan"
	case string(PhaseClueReview):
		return "status-chip status-chip--gold"
	case string(PhaseGuessEntry):
		return "status-chip status-chip--emerald"
	case string(PhaseResolved):
		return "status-chip status-chip--rose"
	default:
		return "status-chip status-chip--ice"
	}
}

func journeyStepClass(round *RoundView, step string) string {
	current := journeyStepOrder("")
	if round != nil {
		current = journeyStepOrder(round.Phase)
	}
	target := journeyStepOrder(step)
	className := "journey-step"
	if current == 0 {
		if target == 1 {
			return className + " is-current"
		}
		return className
	}
	if target < current {
		className += " is-complete"
	}
	if target == current {
		className += " is-current"
	}
	return className
}

func journeyStepOrder(phase string) int {
	switch phase {
	case string(PhaseWordSelection):
		return 1
	case string(PhaseClueEntry):
		return 2
	case string(PhaseClueReview):
		return 3
	case string(PhaseGuessEntry):
		return 4
	case string(PhaseResolved):
		return 5
	default:
		return 0
	}
}

func listOrFallback(values []string, fallback string) string {
	if len(values) == 0 {
		return fallback
	}
	return commaSeparated(values)
}

func firstInitial(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "?"
	}
	for _, word := range strings.Fields(trimmed) {
		for _, r := range word {
			return strings.ToUpper(string(r))
		}
	}
	return strings.ToUpper(string([]rune(trimmed)[0]))
}

func resultBannerClass(result string) string {
	switch strings.ToLower(strings.TrimSpace(result)) {
	case "correct":
		return "result-banner result-banner--correct"
	case "wrong":
		return "result-banner result-banner--wrong"
	case "pass":
		return "result-banner result-banner--pass"
	default:
		return "result-banner"
	}
}
