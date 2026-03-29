package snowflakes

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"time"
)

func (a *App) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/static/", a.static)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("ok")) })
	mux.HandleFunc("/", a.handleIndex)
	mux.HandleFunc("/rooms", a.handleCreateRoom)
	mux.HandleFunc("/rooms/join", a.handleJoinRoom)
	mux.HandleFunc("/rooms/", a.handleRoomRoutes)
	return mux
}

func (a *App) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	token := ensureAuthToken(w, r)
	a.renderTemplate(w, "layout", PageData{Title: "Snowflakes", Token: token})
}

func (a *App) handleCreateRoom(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.NotFound(w, r)
		return
	}
	token := ensureAuthToken(w, r)
	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}
	room := a.createRoom(token, name)
	http.Redirect(w, r, "/rooms/"+room.Code, http.StatusSeeOther)
}

func (a *App) handleJoinRoom(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.NotFound(w, r)
		return
	}
	token := ensureAuthToken(w, r)
	code := strings.ToUpper(strings.TrimSpace(r.FormValue("code")))
	name := strings.TrimSpace(r.FormValue("name"))
	if code == "" || name == "" {
		http.Error(w, "room code and name are required", http.StatusBadRequest)
		return
	}
	room, ok := a.getRoom(code)
	if !ok {
		http.Error(w, "room not found", http.StatusNotFound)
		return
	}
	room.mu.Lock()
	room.join(token, name)
	room.clearFlash()
	room.notify()
	room.mu.Unlock()
	http.Redirect(w, r, "/rooms/"+room.Code, http.StatusSeeOther)
}

func (a *App) handleRoomRoutes(w http.ResponseWriter, r *http.Request) {
	token := ensureAuthToken(w, r)
	path := strings.TrimPrefix(r.URL.Path, "/rooms/")
	if path == "" {
		http.NotFound(w, r)
		return
	}
	parts := strings.Split(path, "/")
	code := strings.ToUpper(parts[0])
	room, ok := a.getRoom(code)
	if !ok {
		http.NotFound(w, r)
		return
	}

	if len(parts) == 1 && r.Method == http.MethodGet {
		a.handleRoomPage(w, r, room, token)
		return
	}
	if len(parts) == 2 && parts[1] == "fragment" && r.Method == http.MethodGet {
		a.handleRoomFragment(w, r, room, token)
		return
	}
	if len(parts) == 2 && parts[1] == "events" && r.Method == http.MethodGet {
		a.handleRoomEvents(w, r, room)
		return
	}
	if len(parts) >= 2 && parts[1] == "actions" && r.Method == http.MethodPost {
		a.handleRoomAction(w, r, room, token, parts[2:])
		return
	}
	if len(parts) >= 2 && parts[1] == "packs" && r.Method == http.MethodPost {
		a.handlePackUpload(w, r, room, token)
		return
	}
	http.NotFound(w, r)
}

func (a *App) handleRoomPage(w http.ResponseWriter, r *http.Request, room *Room, token string) {
	view := a.buildRoomView(room, token)
	a.renderTemplate(w, "layout", PageData{Title: "Snowflakes • " + room.Code, Token: token, Room: &view, HasRoom: true})
}

func (a *App) handleRoomFragment(w http.ResponseWriter, r *http.Request, room *Room, token string) {
	view := a.buildRoomView(room, token)
	a.renderTemplate(w, "room_inner", view)
}

func (a *App) handleRoomEvents(w http.ResponseWriter, r *http.Request, room *Room) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	updates, unsubscribe := room.subscribe()
	defer unsubscribe()
	fmt.Fprintf(w, "event: refresh\ndata: ready\n\n")
	flusher.Flush()
	keepalive := time.NewTicker(20 * time.Second)
	defer keepalive.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-keepalive.C:
			fmt.Fprintf(w, ": ping\n\n")
			flusher.Flush()
		case <-updates:
			fmt.Fprintf(w, "event: refresh\ndata: %d\n\n", time.Now().UnixNano())
			flusher.Flush()
		}
	}
}

func (a *App) handleRoomAction(w http.ResponseWriter, r *http.Request, room *Room, token string, action []string) {
	if len(action) == 0 {
		http.NotFound(w, r)
		return
	}
	room.mu.Lock()
	defer func() {
		room.notify()
		room.mu.Unlock()
		if isAjax(r) {
			w.WriteHeader(http.StatusNoContent)
		} else {
			http.Redirect(w, r, "/rooms/"+room.Code, http.StatusSeeOther)
		}
	}()
	room.clearFlash()

	switch action[0] {
	case "settings":
		a.updateSettings(room, token, r)
	case "start":
		pack, ok := a.resolvePack(room, room.Settings.SelectedPack)
		if !ok {
			room.setFlash("selected pack was not found")
			return
		}
		actionError(room, room.startGame(a.rand, pack))
	case "blind":
		idx := mustInt(r.FormValue("index"), -1)
		actionError(room, room.chooseBlindSlot(token, idx))
	case "vote":
		idx := mustInt(r.FormValue("index"), -1)
		actionError(room, room.castVote(token, idx))
	case "finalize":
		idx := mustInt(r.FormValue("index"), -1)
		actionError(room, room.finalizeVotedWord(token, idx))
	case "clue":
		slot := mustInt(r.FormValue("slot"), 1)
		actionError(room, room.submitClue(token, slot, r.FormValue("text")))
	case "review":
		actionError(room, room.advanceToReview(token))
	case "toggle-clue":
		actionError(room, room.toggleManualInvalid(token, r.FormValue("key")))
	case "guess-phase":
		actionError(room, room.advanceToGuess(token))
	case "guess":
		_, err := room.submitGuess(token, r.FormValue("guess"), r.FormValue("pass") == "1")
		actionError(room, err)
	case "resolve":
		actionError(room, room.adminResolve(token, r.FormValue("result")))
	case "next":
		actionError(room, room.nextRound(token))
	case "participant-role":
		actionError(room, room.setParticipantRole(token, r.FormValue("participant"), ParticipantRole(r.FormValue("role"))))
	case "participant-admin":
		actionError(room, room.setAdmin(token, r.FormValue("participant"), r.FormValue("admin") == "1"))
	default:
		room.setFlash("unknown action")
	}
}

func (a *App) updateSettings(room *Room, token string, r *http.Request) {
	viewer := room.participant(token)
	if viewer == nil || !viewer.Admin {
		room.setFlash("admin required")
		return
	}
	if room.Game != nil && room.Game.CurrentRound != nil && room.Game.CurrentRound.Phase != PhaseResolved && room.Game.Status == GameRunning {
		room.setFlash("settings can only be changed between rounds")
		return
	}
	guessers := mustInt(r.FormValue("guesser_count"), room.Settings.GuesserCount)
	cardPool := mustInt(r.FormValue("card_pool_size"), room.Settings.CardPoolSize)
	choiceSlate := mustInt(r.FormValue("choice_slate_size"), room.Settings.ChoiceSlateSize)
	fixedClues := mustInt(r.FormValue("fixed_clue_slots"), room.Settings.FixedClueSlots)
	if guessers < 1 {
		guessers = 1
	}
	if cardPool < 1 {
		cardPool = 1
	}
	if choiceSlate < 1 {
		choiceSlate = 1
	}
	if choiceSlate > cardPool {
		choiceSlate = cardPool
	}
	if fixedClues < 1 {
		fixedClues = 1
	}
	room.Settings.GuesserCount = guessers
	room.Settings.CardPoolSize = cardPool
	room.Settings.ChoiceSlateSize = choiceSlate
	room.Settings.FixedClueSlots = fixedClues
	if mode := GuessSubmissionMode(r.FormValue("guess_submission_mode")); mode == GuessModeOneEach || mode == GuessModeSpokesperson {
		room.Settings.GuessSubmissionMode = mode
	}
	if mode := GuessResolutionMode(r.FormValue("guess_resolution_mode")); mode == GuessResolutionAdminOnly || mode == GuessResolutionAutoExact {
		room.Settings.GuessResolutionMode = mode
	}
	if mode := WordSelectionMode(r.FormValue("word_selection_mode")); mode == SelectionBlindSlot || mode == SelectionPlayerVote {
		room.Settings.WordSelectionMode = mode
	}
	if mode := ClueSlotsMode(r.FormValue("clue_slots_mode")); mode == ClueSlotsAuto || mode == ClueSlotsFixed {
		room.Settings.ClueSlotsMode = mode
	}
	room.Settings.ShowCardPoolToGuessers = r.FormValue("show_card_pool_to_guessers") == "1"
	if packName := strings.TrimSpace(r.FormValue("selected_pack")); packName != "" {
		room.Settings.SelectedPack = packName
	}
}

func (a *App) handlePackUpload(w http.ResponseWriter, r *http.Request, room *Room, token string) {
	room.mu.Lock()
	defer func() {
		room.notify()
		room.mu.Unlock()
		if isAjax(r) {
			w.WriteHeader(http.StatusNoContent)
		} else {
			http.Redirect(w, r, "/rooms/"+room.Code, http.StatusSeeOther)
		}
	}()
	viewer := room.participant(token)
	if viewer == nil || !viewer.Admin {
		room.setFlash("admin required")
		return
	}
	name := strings.TrimSpace(r.FormValue("pack_name"))
	if name == "" {
		name = "Custom_Pack"
	}
	if err := r.ParseMultipartForm(5 << 20); err != nil && !strings.Contains(err.Error(), "request Content-Type isn't multipart/form-data") {
		room.setFlash(err.Error())
		return
	}
	if text := strings.TrimSpace(r.FormValue("pack_text")); text != "" {
		actionError(room, room.uploadPack(name, strings.NewReader(text)))
		return
	}
	file, _, err := r.FormFile("pack_file")
	if err == nil {
		defer file.Close()
		actionError(room, room.uploadPack(name, file))
		return
	}
	if err != nil && err != http.ErrMissingFile {
		room.setFlash(err.Error())
		return
	}
	room.setFlash("provide pasted words or a text file")
}

func isAjax(r *http.Request) bool {
	return r.Header.Get("X-Requested-With") == "fetch"
}

func ensureAuthToken(w http.ResponseWriter, r *http.Request) string {
	if cookie, err := r.Cookie("snowflakes_auth_token"); err == nil && strings.TrimSpace(cookie.Value) != "" {
		return cookie.Value
	}
	token := randomToken()
	http.SetCookie(w, &http.Cookie{Name: "snowflakes_auth_token", Value: token, Path: "/", MaxAge: 3600 * 24 * 365 * 3, SameSite: http.SameSiteLaxMode})
	return token
}

func randomToken() string {
	var raw [16]byte
	_, _ = rand.Read(raw[:])
	return hex.EncodeToString(raw[:])
}
