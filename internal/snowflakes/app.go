package snowflakes

import (
	"bufio"
	"crypto/rand"
	"embed"
	"errors"
	"fmt"
	"io"
	"io/fs"
	mrand "math/rand"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"unicode"
)

//go:embed static/* wordpacks/*
var embeddedFiles embed.FS

type Config struct {
	Host        string
	Port        int
	PublicURL   string
	WordPackDir string
}

func ConfigFromEnv() Config {
	port := 3400
	if raw := strings.TrimSpace(os.Getenv("PORT")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			port = n
		}
	}
	if raw := strings.TrimSpace(os.Getenv("SNOWFLAKES_PORT")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			port = n
		}
	}

	host := strings.TrimSpace(os.Getenv("NETWORK_ADDRESS"))
	if host == "" {
		host = strings.TrimSpace(os.Getenv("SNOWFLAKES_HOST"))
	}
	if host == "" {
		host = "127.0.0.1"
	}

	publicURL := strings.TrimSpace(os.Getenv("ROOT_URL"))
	if publicURL == "" {
		publicURL = strings.TrimSpace(os.Getenv("SNOWFLAKES_PUBLIC_URL"))
	}
	if publicURL == "" {
		publicURL = "http://justone.pinky.lilf.ir"
	}

	wordPackDir := strings.TrimSpace(os.Getenv("SNOWFLAKES_WORDPACK_DIR"))
	if wordPackDir == "" {
		wordPackDir = filepath.Join(os.Getenv("HOME"), ".snowflakes", "wordpacks")
	}

	return Config{Host: host, Port: port, PublicURL: publicURL, WordPackDir: wordPackDir}
}

func (c Config) BindAddr() string {
	return fmt.Sprintf("%s:%d", c.Host, c.Port)
}

type ParticipantRole string

type GuessSubmissionMode string

type GuessResolutionMode string

type WordSelectionMode string

type ClueSlotsMode string

type Phase string

type GameStatus string

const (
	RolePlayer   ParticipantRole = "player"
	RoleObserver ParticipantRole = "observer"

	GuessModeSpokesperson GuessSubmissionMode = "spokesperson"
	GuessModeOneEach      GuessSubmissionMode = "one_each"

	GuessResolutionAutoExact GuessResolutionMode = "auto_exact_else_admin"
	GuessResolutionAdminOnly GuessResolutionMode = "admin_only"

	SelectionBlindSlot  WordSelectionMode = "blind_slot"
	SelectionPlayerVote WordSelectionMode = "player_vote"

	ClueSlotsAuto  ClueSlotsMode = "auto"
	ClueSlotsFixed ClueSlotsMode = "fixed"

	PhaseWordSelection Phase = "word_selection"
	PhaseClueEntry     Phase = "clue_entry"
	PhaseClueReview    Phase = "clue_review"
	PhaseGuessEntry    Phase = "guess_entry"
	PhaseResolved      Phase = "round_resolved"

	GameLobby    GameStatus = "lobby"
	GameRunning  GameStatus = "running"
	GameFinished GameStatus = "finished"
)

type Participant struct {
	Token       string
	Name        string
	Role        ParticipantRole
	Admin       bool
	Creator     bool
	PendingRole *ParticipantRole
}

type RoomSettings struct {
	GuesserCount           int
	ClueSlotsMode          ClueSlotsMode
	FixedClueSlots         int
	GuessSubmissionMode    GuessSubmissionMode
	GuessResolutionMode    GuessResolutionMode
	WordSelectionMode      WordSelectionMode
	CardPoolSize           int
	ChoiceSlateSize        int
	ShowCardPoolToGuessers bool
	SelectedPack           string
}

type WordPack struct {
	Name   string
	Words  []string
	Source string
}

type RoundCard struct {
	Pool  []string
	Slate []string
}

type ClueSubmission struct {
	PlayerToken string
	Slot        int
	Text        string
}

type Round struct {
	Phase         Phase
	Card          RoundCard
	TargetIndex   int
	TargetWord    string
	VotesByToken  map[string]int
	Clues         map[string]ClueSubmission
	ManualInvalid map[string]bool
	Guesses       map[string]string
	PassByToken   map[string]bool
	Result        string
}

type Game struct {
	Status       GameStatus
	Deck         []RoundCard
	CurrentIndex int
	Won          int
	CurrentRound *Round
}

type Room struct {
	mu           sync.RWMutex
	Code         string
	CreatorToken string
	Participants map[string]*Participant
	Order        []string
	Settings     RoomSettings
	Game         *Game
	CustomPacks  map[string]WordPack
	flash        string
	subscribers  map[chan struct{}]struct{}
	revision     int
}

type App struct {
	cfg    Config
	static http.Handler
	mu     sync.RWMutex
	rooms  map[string]*Room
	packs  map[string]WordPack
	rand   *mrand.Rand
}

func NewApp(cfg Config) (*App, error) {
	staticFS, err := fs.Sub(embeddedFiles, "static")
	if err != nil {
		return nil, err
	}

	packs, err := loadWordPacks(cfg.WordPackDir)
	if err != nil {
		return nil, err
	}

	return &App{
		cfg:    cfg,
		static: http.StripPrefix("/static/", http.FileServer(http.FS(staticFS))),
		rooms:  map[string]*Room{},
		packs:  packs,
		rand:   mrand.New(mrand.NewSource(seedMathRand())),
	}, nil
}

func seedMathRand() int64 {
	var b [8]byte
	if _, err := rand.Read(b[:]); err == nil {
		var out int64
		for _, c := range b {
			out = (out << 8) | int64(c)
		}
		if out != 0 {
			return out
		}
	}
	return 1
}

func defaultRoomSettings() RoomSettings {
	return RoomSettings{
		GuesserCount:           1,
		ClueSlotsMode:          ClueSlotsAuto,
		FixedClueSlots:         1,
		GuessSubmissionMode:    GuessModeSpokesperson,
		GuessResolutionMode:    GuessResolutionAutoExact,
		WordSelectionMode:      SelectionBlindSlot,
		CardPoolSize:           5,
		ChoiceSlateSize:        5,
		ShowCardPoolToGuessers: false,
		SelectedPack:           "English_Snowflakes_1",
	}
}

func loadWordPacks(dir string) (map[string]WordPack, error) {
	packs := map[string]WordPack{}
	defaultData, err := embeddedFiles.ReadFile("wordpacks/English_Snowflakes_1.txt")
	if err != nil {
		return nil, err
	}
	defaultPack, err := parseWordPackText("English_Snowflakes_1", string(defaultData), "embedded")
	if err != nil {
		return nil, err
	}
	packs[defaultPack.Name] = defaultPack

	entries, err := os.ReadDir(dir)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if !strings.HasSuffix(strings.ToLower(entry.Name()), ".txt") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		name := strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name()))
		pack, err := parseWordPackText(name, string(data), path)
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", entry.Name(), err)
		}
		packs[pack.Name] = pack
	}
	return packs, nil
}

func parseWordPackText(name, text, source string) (WordPack, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return WordPack{}, errors.New("pack name is required")
	}
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.TrimPrefix(text, "\ufeff")
	seen := map[string]struct{}{}
	words := make([]string, 0)
	s := bufio.NewScanner(strings.NewReader(text))
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if line == "" {
			continue
		}
		norm := normalizeText(line)
		if _, ok := seen[norm]; ok {
			continue
		}
		seen[norm] = struct{}{}
		words = append(words, line)
	}
	if err := s.Err(); err != nil {
		return WordPack{}, err
	}
	if len(words) == 0 {
		return WordPack{}, errors.New("word pack has no entries")
	}
	return WordPack{Name: name, Words: words, Source: source}, nil
}

func normalizeText(s string) string {
	s = strings.TrimSpace(strings.ToLower(s))
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return ' '
		}
		return r
	}, s)
}

func clueKey(token string, slot int) string {
	return fmt.Sprintf("%s:%d", token, slot)
}

func (r *Room) notify() {
	r.revision++
	for ch := range r.subscribers {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

func (r *Room) subscribe() (chan struct{}, func()) {
	r.mu.Lock()
	defer r.mu.Unlock()
	ch := make(chan struct{}, 1)
	if r.subscribers == nil {
		r.subscribers = map[chan struct{}]struct{}{}
	}
	r.subscribers[ch] = struct{}{}
	return ch, func() {
		r.mu.Lock()
		defer r.mu.Unlock()
		delete(r.subscribers, ch)
		close(ch)
	}
}

func (a *App) listGlobalPacks() []WordPack {
	a.mu.RLock()
	defer a.mu.RUnlock()
	packs := make([]WordPack, 0, len(a.packs))
	for _, pack := range a.packs {
		packs = append(packs, pack)
	}
	sort.Slice(packs, func(i, j int) bool { return strings.ToLower(packs[i].Name) < strings.ToLower(packs[j].Name) })
	return packs
}

func (a *App) createRoom(token, name string) *Room {
	a.mu.Lock()
	defer a.mu.Unlock()
	for {
		code := randomRoomCode(a.rand)
		if _, exists := a.rooms[code]; exists {
			continue
		}
		p := &Participant{Token: token, Name: name, Role: RolePlayer, Admin: true, Creator: true}
		room := &Room{
			Code:         code,
			CreatorToken: token,
			Participants: map[string]*Participant{token: p},
			Order:        []string{token},
			Settings:     defaultRoomSettings(),
			Game:         &Game{Status: GameLobby},
			CustomPacks:  map[string]WordPack{},
			subscribers:  map[chan struct{}]struct{}{},
		}
		a.rooms[code] = room
		return room
	}
}

func randomRoomCode(r *mrand.Rand) string {
	const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	buf := make([]byte, 5)
	for i := range buf {
		buf[i] = alphabet[r.Intn(len(alphabet))]
	}
	return string(buf)
}

func (a *App) getRoom(code string) (*Room, bool) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	room, ok := a.rooms[strings.ToUpper(strings.TrimSpace(code))]
	return room, ok
}

func (a *App) roomPacks(room *Room) []WordPack {
	global := a.listGlobalPacks()
	room.mu.RLock()
	defer room.mu.RUnlock()
	packs := append([]WordPack{}, global...)
	for _, p := range room.CustomPacks {
		packs = append(packs, p)
	}
	sort.Slice(packs, func(i, j int) bool { return strings.ToLower(packs[i].Name) < strings.ToLower(packs[j].Name) })
	return packs
}

func (a *App) resolvePack(room *Room, name string) (WordPack, bool) {
	room.mu.RLock()
	if pack, ok := room.CustomPacks[name]; ok {
		room.mu.RUnlock()
		return pack, true
	}
	room.mu.RUnlock()
	a.mu.RLock()
	defer a.mu.RUnlock()
	pack, ok := a.packs[name]
	return pack, ok
}

func (r *Room) participant(token string) *Participant { return r.Participants[token] }

func (r *Room) playerOrder() []string {
	out := make([]string, 0, len(r.Order))
	for _, token := range r.Order {
		p := r.Participants[token]
		if p != nil && p.Role == RolePlayer {
			out = append(out, token)
		}
	}
	return out
}

func (r *Room) flashMessage() string { return r.flash }
func (r *Room) setFlash(msg string)  { r.flash = msg }
func (r *Room) clearFlash()          { r.flash = "" }

func (r *Room) effectiveClueSlots(round *Round) int {
	if r.Settings.ClueSlotsMode == ClueSlotsFixed {
		if r.Settings.FixedClueSlots > 0 {
			return r.Settings.FixedClueSlots
		}
		return 1
	}
	cluegivers := r.eligibleCluegivers(round)
	if len(cluegivers) <= 3 {
		return 2
	}
	return 1
}

func (r *Room) activeGuessers(round *Round) []string {
	players := r.playerOrder()
	if len(players) == 0 {
		return nil
	}
	count := r.Settings.GuesserCount
	if count < 1 {
		count = 1
	}
	if count >= len(players) {
		count = max(1, len(players)-1)
	}
	if count < 1 {
		count = 1
	}
	start := 0
	if r.Game != nil {
		start = r.Game.CurrentIndex % len(players)
	}
	out := make([]string, 0, count)
	for i := 0; i < count; i++ {
		out = append(out, players[(start+i)%len(players)])
	}
	return out
}

func (r *Room) eligibleCluegivers(round *Round) []string {
	guessers := map[string]struct{}{}
	for _, token := range r.activeGuessers(round) {
		guessers[token] = struct{}{}
	}
	players := r.playerOrder()
	out := make([]string, 0, len(players))
	for _, token := range players {
		if _, ok := guessers[token]; ok {
			continue
		}
		out = append(out, token)
	}
	return out
}

func (r *Room) applyPendingRoles() {
	for _, p := range r.Participants {
		if p.PendingRole != nil {
			p.Role = *p.PendingRole
			p.PendingRole = nil
			if p.Role == RolePlayer && !slices.Contains(r.Order, p.Token) {
				r.Order = append(r.Order, p.Token)
			}
		}
	}
}

func buildDeck(rng *mrand.Rand, pack WordPack, settings RoomSettings) ([]RoundCard, error) {
	need := 13 * settings.CardPoolSize
	if settings.CardPoolSize < 1 {
		return nil, errors.New("card pool size must be positive")
	}
	if settings.ChoiceSlateSize < 1 || settings.ChoiceSlateSize > settings.CardPoolSize {
		return nil, errors.New("choice slate size must be between 1 and card pool size")
	}
	if len(pack.Words) < need {
		return nil, fmt.Errorf("pack %s needs at least %d unique words for current settings, got %d", pack.Name, need, len(pack.Words))
	}
	idxs := rng.Perm(len(pack.Words))[:need]
	deck := make([]RoundCard, 0, 13)
	for i := 0; i < 13; i++ {
		pool := make([]string, 0, settings.CardPoolSize)
		for j := 0; j < settings.CardPoolSize; j++ {
			pool = append(pool, pack.Words[idxs[i*settings.CardPoolSize+j]])
		}
		slate := append([]string{}, pool...)
		if settings.ChoiceSlateSize < len(slate) {
			choiceIdxs := rng.Perm(len(pool))[:settings.ChoiceSlateSize]
			sort.Ints(choiceIdxs)
			slate = make([]string, 0, settings.ChoiceSlateSize)
			for _, idx := range choiceIdxs {
				slate = append(slate, pool[idx])
			}
		}
		deck = append(deck, RoundCard{Pool: pool, Slate: slate})
	}
	return deck, nil
}

func (r *Room) startGame(rng *mrand.Rand, pack WordPack) error {
	players := r.playerOrder()
	if len(players) < 2 {
		return errors.New("need at least 2 players to start")
	}
	if r.Settings.GuesserCount >= len(players) {
		return errors.New("guesser count must be lower than player count")
	}
	deck, err := buildDeck(rng, pack, r.Settings)
	if err != nil {
		return err
	}
	r.Game = &Game{Status: GameRunning, Deck: deck, CurrentIndex: 0}
	r.clearFlash()
	return r.setupRound()
}

func (r *Room) setupRound() error {
	r.applyPendingRoles()
	if r.Game == nil || r.Game.CurrentIndex >= len(r.Game.Deck) {
		return errors.New("no remaining round")
	}
	card := r.Game.Deck[r.Game.CurrentIndex]
	r.Game.CurrentRound = &Round{
		Phase:         PhaseWordSelection,
		Card:          card,
		VotesByToken:  map[string]int{},
		Clues:         map[string]ClueSubmission{},
		ManualInvalid: map[string]bool{},
		Guesses:       map[string]string{},
		PassByToken:   map[string]bool{},
	}
	r.Game.Status = GameRunning
	return nil
}

func (r *Room) round() *Round {
	if r.Game == nil {
		return nil
	}
	return r.Game.CurrentRound
}

func (r *Room) chooseBlindSlot(token string, idx int) error {
	round := r.round()
	if round == nil || round.Phase != PhaseWordSelection {
		return errors.New("not in word selection")
	}
	guessers := r.activeGuessers(round)
	if len(guessers) == 0 || guessers[0] != token {
		return errors.New("only the spokesperson guesser can choose the slot")
	}
	if idx < 0 || idx >= len(round.Card.Slate) {
		return errors.New("invalid slot")
	}
	round.TargetIndex = idx
	round.TargetWord = round.Card.Slate[idx]
	round.Phase = PhaseClueEntry
	return nil
}

func (r *Room) castVote(token string, idx int) error {
	round := r.round()
	if round == nil || round.Phase != PhaseWordSelection || r.Settings.WordSelectionMode != SelectionPlayerVote {
		return errors.New("voting is not available")
	}
	if !slices.Contains(r.eligibleCluegivers(round), token) {
		return errors.New("only clue-givers can vote")
	}
	if idx < 0 || idx >= len(round.Card.Slate) {
		return errors.New("invalid choice")
	}
	round.VotesByToken[token] = idx
	return nil
}

func (r *Room) finalizeVotedWord(requester string, idx int) error {
	p := r.Participants[requester]
	if p == nil || !p.Admin {
		return errors.New("admin required")
	}
	round := r.round()
	if round == nil || round.Phase != PhaseWordSelection || r.Settings.WordSelectionMode != SelectionPlayerVote {
		return errors.New("not in player vote mode")
	}
	if idx < 0 || idx >= len(round.Card.Slate) {
		return errors.New("invalid choice")
	}
	round.TargetIndex = idx
	round.TargetWord = round.Card.Slate[idx]
	round.Phase = PhaseClueEntry
	return nil
}

func (r *Room) submitClue(token string, slot int, text string) error {
	round := r.round()
	if round == nil || round.Phase != PhaseClueEntry {
		return errors.New("not accepting clues")
	}
	if !slices.Contains(r.eligibleCluegivers(round), token) {
		return errors.New("not eligible to submit clues")
	}
	maxSlots := r.effectiveClueSlots(round)
	if slot < 1 || slot > maxSlots {
		return errors.New("invalid clue slot")
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return errors.New("clue cannot be empty")
	}
	round.Clues[clueKey(token, slot)] = ClueSubmission{PlayerToken: token, Slot: slot, Text: text}
	return nil
}

func (r *Room) allCluesSubmitted(round *Round) bool {
	if round == nil || round.Phase != PhaseClueEntry {
		return false
	}
	slots := r.effectiveClueSlots(round)
	for _, token := range r.eligibleCluegivers(round) {
		for slot := 1; slot <= slots; slot++ {
			if _, ok := round.Clues[clueKey(token, slot)]; !ok {
				return false
			}
		}
	}
	return len(r.eligibleCluegivers(round)) > 0
}

func (r *Room) advanceToReview(requester string) error {
	p := r.Participants[requester]
	if p == nil || !p.Admin {
		return errors.New("admin required")
	}
	round := r.round()
	if round == nil || round.Phase != PhaseClueEntry {
		return errors.New("not in clue entry")
	}
	if !r.allCluesSubmitted(round) {
		return errors.New("all clue slots must be submitted first")
	}
	round.Phase = PhaseClueReview
	return nil
}

func (r *Room) toggleManualInvalid(requester, key string) error {
	p := r.Participants[requester]
	if p == nil || !p.Admin {
		return errors.New("admin required")
	}
	round := r.round()
	if round == nil || round.Phase != PhaseClueReview {
		return errors.New("not in clue review")
	}
	if _, ok := round.Clues[key]; !ok {
		return errors.New("unknown clue")
	}
	round.ManualInvalid[key] = !round.ManualInvalid[key]
	return nil
}

func (r *Room) advanceToGuess(requester string) error {
	p := r.Participants[requester]
	if p == nil || !p.Admin {
		return errors.New("admin required")
	}
	round := r.round()
	if round == nil || round.Phase != PhaseClueReview {
		return errors.New("not in clue review")
	}
	round.Phase = PhaseGuessEntry
	return nil
}

func (r *Room) spokesperson(round *Round) string {
	guessers := r.activeGuessers(round)
	if len(guessers) == 0 {
		return ""
	}
	return guessers[0]
}

func (r *Room) submitGuess(token, guess string, pass bool) (bool, error) {
	round := r.round()
	if round == nil || round.Phase != PhaseGuessEntry {
		return false, errors.New("not accepting guesses")
	}
	guessers := r.activeGuessers(round)
	if !slices.Contains(guessers, token) {
		return false, errors.New("not an active guesser")
	}
	if r.Settings.GuessSubmissionMode == GuessModeSpokesperson && token != r.spokesperson(round) {
		return false, errors.New("only the spokesperson can submit the guess")
	}
	if pass {
		round.PassByToken[token] = true
		if r.Settings.GuessSubmissionMode == GuessModeSpokesperson {
			return true, r.resolveRound("pass")
		}
		return false, nil
	}
	guess = strings.TrimSpace(guess)
	if guess == "" {
		return false, errors.New("guess cannot be empty")
	}
	round.Guesses[token] = guess
	if r.Settings.GuessResolutionMode == GuessResolutionAutoExact {
		for _, g := range round.Guesses {
			if normalizeText(g) == normalizeText(round.TargetWord) {
				return true, r.resolveRound("correct")
			}
		}
	}
	return false, nil
}

func (r *Room) resolveRound(kind string) error {
	if r.Game == nil || r.Game.CurrentRound == nil {
		return errors.New("no current round")
	}
	round := r.Game.CurrentRound
	round.Phase = PhaseResolved
	round.Result = kind
	switch kind {
	case "correct":
		r.Game.Won++
		r.Game.CurrentIndex++
	case "pass":
		r.Game.CurrentIndex++
	case "wrong":
		r.Game.CurrentIndex++
		if r.Game.CurrentIndex < len(r.Game.Deck) {
			r.Game.CurrentIndex++
		} else if r.Game.Won > 0 {
			r.Game.Won--
		}
	default:
		return errors.New("unknown round result")
	}
	if r.Game.CurrentIndex >= len(r.Game.Deck) {
		r.Game.Status = GameFinished
	} else {
		r.Game.Status = GameRunning
	}
	return nil
}

func (r *Room) nextRound(requester string) error {
	p := r.Participants[requester]
	if p == nil || !p.Admin {
		return errors.New("admin required")
	}
	if r.Game == nil {
		return errors.New("game not started")
	}
	if r.Game.Status == GameFinished {
		return errors.New("game is finished")
	}
	round := r.round()
	if round == nil || round.Phase != PhaseResolved {
		return errors.New("current round is not resolved")
	}
	return r.setupRound()
}

func (r *Room) adminResolve(requester, kind string) error {
	p := r.Participants[requester]
	if p == nil || !p.Admin {
		return errors.New("admin required")
	}
	round := r.round()
	if round == nil || round.Phase != PhaseGuessEntry {
		return errors.New("not in guess phase")
	}
	return r.resolveRound(kind)
}

func (r *Room) setParticipantRole(requester, target string, role ParticipantRole) error {
	admin := r.Participants[requester]
	participant := r.Participants[target]
	if admin == nil || !admin.Admin {
		return errors.New("admin required")
	}
	if participant == nil {
		return errors.New("participant not found")
	}
	if participant.Role == role {
		return nil
	}
	round := r.round()
	if round != nil && round.Phase != PhaseResolved {
		participant.PendingRole = &role
		return nil
	}
	participant.Role = role
	participant.PendingRole = nil
	if role == RolePlayer && !slices.Contains(r.Order, participant.Token) {
		r.Order = append(r.Order, participant.Token)
	}
	return nil
}

func (r *Room) setAdmin(requester, target string, adminValue bool) error {
	requesterP := r.Participants[requester]
	targetP := r.Participants[target]
	if requesterP == nil || !requesterP.Admin {
		return errors.New("admin required")
	}
	if targetP == nil {
		return errors.New("participant not found")
	}
	if targetP.Creator && !adminValue {
		return errors.New("creator always remains admin")
	}
	targetP.Admin = adminValue || targetP.Creator
	return nil
}

func (r *Room) join(token, name string) {
	if existing := r.Participants[token]; existing != nil {
		if strings.TrimSpace(name) != "" {
			existing.Name = strings.TrimSpace(name)
		}
		return
	}
	role := RolePlayer
	if round := r.round(); round != nil && round.Phase != PhaseResolved {
		if round.Phase != PhaseWordSelection && round.Phase != PhaseClueEntry {
			role = RoleObserver
		}
	}
	p := &Participant{Token: token, Name: name, Role: role}
	r.Participants[token] = p
	if role == RolePlayer {
		r.Order = append(r.Order, token)
	}
}

func (r *Room) uploadPack(name string, data io.Reader) error {
	bytes, err := io.ReadAll(data)
	if err != nil {
		return err
	}
	pack, err := parseWordPackText(name, string(bytes), "room upload")
	if err != nil {
		return err
	}
	r.CustomPacks[pack.Name] = pack
	return nil
}

func detectInvalidClues(round *Round) map[string]bool {
	counts := map[string]int{}
	for key, clue := range round.Clues {
		norm := normalizeText(clue.Text)
		if norm == "" {
			continue
		}
		counts[norm]++
		_ = key
	}
	invalid := map[string]bool{}
	for key, clue := range round.Clues {
		norm := normalizeText(clue.Text)
		if round.ManualInvalid[key] || counts[norm] > 1 {
			invalid[key] = true
		}
	}
	return invalid
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
