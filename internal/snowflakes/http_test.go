package snowflakes

import (
	"bufio"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestIndexRendersLandingPage(t *testing.T) {
	app := newTestApp(t)
	rr := performRequest(t, app.Handler(), http.MethodGet, "/", "", nil, nil)
	assertStatus(t, rr.Code, http.StatusOK)

	body := rr.Body.String()
	assertContainsAll(t, body,
		"<!doctype html>",
		"/static/styles.css",
		"/static/app.js",
		"action=\"/rooms\"",
		"action=\"/rooms/join\"",
		"Create room",
		"Join room",
	)
	assertNotContainsAny(t, body, "id=\"room-root\"", "data-room-code=")
	if cookie := rr.Result().Cookies(); len(cookie) == 0 {
		t.Fatal("expected auth cookie to be set")
	}
}

func TestRoomPageAndFragmentRender(t *testing.T) {
	app := newTestApp(t)
	room := app.createRoom("creator-token", "Alice")
	handler := app.Handler()

	pageRR := performRequest(t, handler, http.MethodGet, "/rooms/"+room.Code, "creator-token", nil, nil)
	assertStatus(t, pageRR.Code, http.StatusOK)

	pageBody := pageRR.Body.String()
	assertContainsAll(t, pageBody,
		"<!doctype html>",
		"id=\"room-root\"",
		"data-room-code=\""+room.Code+"\"",
		"You are here",
		"<strong>Alice</strong>",
		"Room "+room.Code,
		"data-copy-text=\"http://example.com/rooms/"+room.Code+"\"",
		"Copy link",
		"action=\"/rooms/"+room.Code+"/actions/start\"",
		"data-ajax=\"true\"",
	)

	fragmentRR := performRequest(t, handler, http.MethodGet, "/rooms/"+room.Code+"/fragment", "creator-token", nil, nil)
	assertStatus(t, fragmentRR.Code, http.StatusOK)

	fragmentBody := fragmentRR.Body.String()
	assertNotContainsAny(t, fragmentBody, "<!doctype html>", "<html", "<body")
	assertContainsAll(t, fragmentBody, "class=\"room-shell\"", "Room "+room.Code, "Copy link", "Ready to start")
}

func TestRoomEventsSendsInitialRefresh(t *testing.T) {
	app := newTestApp(t)
	room := app.createRoom("creator-token", "Alice")
	server := httptest.NewServer(app.Handler())
	defer server.Close()

	resp, err := server.Client().Get(server.URL + "/rooms/" + room.Code + "/events")
	if err != nil {
		t.Fatalf("GET events returned error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected events status %d, got %d", http.StatusOK, resp.StatusCode)
	}
	if got := resp.Header.Get("Content-Type"); !strings.Contains(got, "text/event-stream") {
		t.Fatalf("expected text/event-stream content type, got %q", got)
	}

	reader := bufio.NewReader(resp.Body)
	var lines []string
	for len(lines) < 3 {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("reading SSE stream failed: %v", err)
		}
		lines = append(lines, line)
		if line == "\n" {
			break
		}
	}
	got := strings.Join(lines, "")
	if !strings.Contains(got, "event: refresh\n") || !strings.Contains(got, "data: ready\n") {
		t.Fatalf("expected initial refresh event, got %q", got)
	}
}

func TestStaticAssetsServeContentTypes(t *testing.T) {
	app := newTestApp(t)
	for _, tc := range []struct {
		name        string
		path        string
		contentType string
	}{
		{name: "javascript", path: "/static/app.js", contentType: "javascript"},
		{name: "stylesheet", path: "/static/styles.css", contentType: "text/css"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rr := performRequest(t, app.Handler(), http.MethodGet, tc.path, "", nil, nil)
			assertStatus(t, rr.Code, http.StatusOK)
			if got := rr.Header().Get("Content-Type"); !strings.Contains(got, tc.contentType) {
				t.Fatalf("expected content type containing %q, got %q", tc.contentType, got)
			}
			if strings.TrimSpace(rr.Body.String()) == "" {
				t.Fatalf("expected non-empty body for %s", tc.path)
			}
		})
	}
}

func TestStartActionSupportsAjaxAndBrowserRequests(t *testing.T) {
	for _, tc := range []struct {
		name       string
		ajax       bool
		wantStatus int
	}{
		{name: "ajax", ajax: true, wantStatus: http.StatusNoContent},
		{name: "browser", ajax: false, wantStatus: http.StatusSeeOther},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app := newTestApp(t)
			room := app.createRoom(aliceToken, "Alice")
			room.mu.Lock()
			room.join(bobToken, "Bob")
			room.mu.Unlock()

			rr := performFormRequest(t, app.Handler(), "/rooms/"+room.Code+"/actions/start", aliceToken, "", tc.ajax)
			assertStatus(t, rr.Code, tc.wantStatus)
			if !tc.ajax {
				if got := rr.Header().Get("Location"); got != "/rooms/"+room.Code {
					t.Fatalf("expected redirect to room page, got %q", got)
				}
			}

			room.mu.RLock()
			defer room.mu.RUnlock()
			if room.Game == nil || room.Game.Status != GameRunning || room.Game.CurrentRound == nil {
				t.Fatalf("expected running game with active round, got %#v", room.Game)
			}
		})
	}
}

func TestAnonymousRoomPageShowsJoinFormInsteadOfViewerState(t *testing.T) {
	app := newTestApp(t)
	room := app.createRoom(aliceToken, "Alice")

	rr := performRequest(t, app.Handler(), http.MethodGet, "/rooms/"+room.Code, daveToken, nil, nil)
	assertStatus(t, rr.Code, http.StatusOK)

	body := rr.Body.String()
	assertContainsAll(t, body,
		"action=\"/rooms/join\"",
		"name=\"code\" value=\""+room.Code+"\"",
		"Join room",
	)
	assertNotContainsAny(t, body, "You are here", "<strong>Dave</strong>")
}

func TestAdminPickGuesserFragmentHidesChoiceWords(t *testing.T) {
	app := newTestApp(t)
	room := newRoundTestRoom(t, app)
	withRoomLock(t, room, func(room *Room, round *Round) {
		room.Settings.WordSelectionMode = SelectionAdminPick
		round.Phase = PhaseWordSelection
		round.TargetWord = ""
	})

	guesserRR := performRequest(t, app.Handler(), http.MethodGet, "/rooms/"+room.Code+"/fragment", aliceToken, nil, nil)
	guesserBody := guesserRR.Body.String()
	assertContainsAll(t, guesserBody, "Waiting for the round controller to choose the hidden word.")
	assertNotContainsAny(t, guesserBody, "Apple", "Pear", "Peach", "Choose this word")

	cluegiverRR := performRequest(t, app.Handler(), http.MethodGet, "/rooms/"+room.Code+"/fragment", bobToken, nil, nil)
	cluegiverBody := cluegiverRR.Body.String()
	assertContainsAll(t, cluegiverBody, "Apple", "Pear", "Choose this word")

	observerRR := performRequest(t, app.Handler(), http.MethodGet, "/rooms/"+room.Code+"/fragment", daveToken, nil, nil)
	observerBody := observerRR.Body.String()
	assertContainsAll(t, observerBody, "Waiting for word selection.")
	assertNotContainsAny(t, observerBody, "Apple", "Pear", "Peach", "Choose this word", "Vote")
}

func TestPlayerVoteFragmentShowsVoteAndFinalizeContracts(t *testing.T) {
	app := newTestApp(t)
	room := newRoundTestRoom(t, app)
	withRoomLock(t, room, func(room *Room, round *Round) {
		room.Settings.WordSelectionMode = SelectionPlayerVote
		round.Phase = PhaseWordSelection
		round.VotesByToken[bobToken] = 1
		round.VotesByToken[caraToken] = 0
	})

	controllerRR := performRequest(t, app.Handler(), http.MethodGet, "/rooms/"+room.Code+"/fragment", bobToken, nil, nil)
	controllerBody := controllerRR.Body.String()
	assertContainsAll(t, controllerBody, "Vote for the secret target word.", "Votes: 1", ">Voted</button>", "Choose this word")

	cluegiverRR := performRequest(t, app.Handler(), http.MethodGet, "/rooms/"+room.Code+"/fragment", caraToken, nil, nil)
	cluegiverBody := cluegiverRR.Body.String()
	assertContainsAll(t, cluegiverBody, "Vote for the secret target word.", ">Voted</button>")
	assertNotContainsAny(t, cluegiverBody, "Choose this word")
}

func TestClueEntryFragmentContractsByRole(t *testing.T) {
	app := newTestApp(t)
	room := newRoundTestRoom(t, app)
	withRoomLock(t, room, func(room *Room, round *Round) {
		round.Phase = PhaseClueEntry
		round.TargetIndex = 1
		round.TargetWord = "Pear"
	})

	cluegiverRR := performRequest(t, app.Handler(), http.MethodGet, "/rooms/"+room.Code+"/fragment", bobToken, nil, nil)
	cluegiverBody := cluegiverRR.Body.String()
	assertContainsAll(t, cluegiverBody, "name=\"clue_1\"", "name=\"clue_2\"", "pattern=\".*\\\\S.*\"", ">Submit clues</button>", "Your clues: 0 / 2")
	assertContainsCount(t, cluegiverBody, ">Submit clues</button>", 1)

	guesserRR := performRequest(t, app.Handler(), http.MethodGet, "/rooms/"+room.Code+"/fragment", aliceToken, nil, nil)
	guesserBody := guesserRR.Body.String()
	assertContainsAll(t, guesserBody, "Stay blind while the clue-givers enter their clues.")
	assertNotContainsAny(t, guesserBody, "name=\"clue_1\"", ">Submit clues</button>")

	observerRR := performRequest(t, app.Handler(), http.MethodGet, "/rooms/"+room.Code+"/fragment", daveToken, nil, nil)
	observerBody := observerRR.Body.String()
	assertContainsAll(t, observerBody, "Waiting for clue-givers to finish submitting clues.")
	assertNotContainsAny(t, observerBody, "name=\"clue_1\"", ">Submit clues</button>")
}

func TestClueBulkSubmitActionSavesAllSlots(t *testing.T) {
	app := newTestApp(t)
	room := app.createRoom(aliceToken, "Alice")
	room.mu.Lock()
	room.join(bobToken, "Bob")
	room.Game = &Game{
		Status:       GameRunning,
		CurrentIndex: 0,
		Deck:         []RoundCard{testRoundCard()},
		CurrentRound: testRound(),
	}
	room.Game.CurrentRound.Phase = PhaseClueEntry
	room.Game.CurrentRound.TargetIndex = 1
	room.Game.CurrentRound.TargetWord = "Pear"
	room.assignTemporaryRoundController(room.Game.CurrentRound)
	room.mu.Unlock()

	rr := performFormRequest(t, app.Handler(), "/rooms/"+room.Code+"/actions/clue", bobToken, "clue_1=orchard&clue_2=green", true)
	assertStatus(t, rr.Code, http.StatusNoContent)

	room.mu.RLock()
	defer room.mu.RUnlock()
	if got := len(room.Game.CurrentRound.Clues); got != 2 {
		t.Fatalf("expected 2 saved clues, got %d", got)
	}
	if !room.allCluesSubmitted(room.Game.CurrentRound) {
		t.Fatal("expected cluegiver to have completed all clue slots")
	}
	if room.Game.CurrentRound.Phase != PhaseClueReview {
		t.Fatalf("expected round to auto-advance to clue review, got %s", room.Game.CurrentRound.Phase)
	}
}

func TestClueReviewHidesDuplicateToggleButtons(t *testing.T) {
	app := newTestApp(t)
	room := newRoundTestRoom(t, app)
	withRoomLock(t, room, func(room *Room, round *Round) {
		round.Phase = PhaseClueReview
		round.TargetIndex = 1
		round.TargetWord = "Pear"
		round.Clues[clueKey(bobToken, 1)] = ClueSubmission{PlayerToken: bobToken, Slot: 1, Text: "Orchard"}
		round.Clues[clueKey(caraToken, 1)] = ClueSubmission{PlayerToken: caraToken, Slot: 1, Text: " orchard "}
		round.Clues[clueKey(caraToken, 2)] = ClueSubmission{PlayerToken: caraToken, Slot: 2, Text: "Green"}
		round.ManualInvalid[clueKey(caraToken, 2)] = true
	})

	controllerRR := performRequest(t, app.Handler(), http.MethodGet, "/rooms/"+room.Code+"/fragment", bobToken, nil, nil)
	controllerBody := controllerRR.Body.String()
	assertContainsAll(t, controllerBody, "duplicate", ">Restore</button>", "Reveal valid clues")
	assertContainsCount(t, controllerBody, ">Restore</button>", 1)
	assertContainsCount(t, controllerBody, ">Toggle invalid</button>", 0)

	cluegiverRR := performRequest(t, app.Handler(), http.MethodGet, "/rooms/"+room.Code+"/fragment", caraToken, nil, nil)
	cluegiverBody := cluegiverRR.Body.String()
	assertContainsAll(t, cluegiverBody, "duplicate")
	assertNotContainsAny(t, cluegiverBody, ">Restore</button>", ">Toggle invalid</button>", "Reveal valid clues")
}

func TestParticipantListShowsRoundControllerIcon(t *testing.T) {
	app := newTestApp(t)
	room := newRoundTestRoom(t, app)
	rr := performRequest(t, app.Handler(), http.MethodGet, "/rooms/"+room.Code+"/fragment", aliceToken, nil, nil)
	body := rr.Body.String()
	assertContainsCount(t, body, "aria-label=\"Round controller\"", 1)
	assertContainsAll(t, body, "🕹️")
}

func TestGuessPhaseRenderHonorsSubmissionModes(t *testing.T) {
	for _, tc := range []struct {
		name                string
		mode                GuessSubmissionMode
		bobShouldSeeGuessUI bool
	}{
		{name: "spokesperson", mode: GuessModeSpokesperson, bobShouldSeeGuessUI: false},
		{name: "one-each", mode: GuessModeOneEach, bobShouldSeeGuessUI: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app := newTestApp(t)
			room := newRoundTestRoom(t, app)
			withRoomLock(t, room, func(room *Room, round *Round) {
				room.Settings.GuesserCount = 2
				room.Settings.GuessSubmissionMode = tc.mode
				round.Phase = PhaseGuessEntry
				round.TargetIndex = 1
				round.TargetWord = "Pear"
				round.Clues[clueKey(caraToken, 1)] = ClueSubmission{PlayerToken: caraToken, Slot: 1, Text: "orchard"}
			})

			aliceRR := performRequest(t, app.Handler(), http.MethodGet, "/rooms/"+room.Code+"/fragment", aliceToken, nil, nil)
			assertContainsAll(t, aliceRR.Body.String(), "name=\"guess\"", ">Submit guess</button>", ">Pass</button>")

			bobRR := performRequest(t, app.Handler(), http.MethodGet, "/rooms/"+room.Code+"/fragment", bobToken, nil, nil)
			bobBody := bobRR.Body.String()
			if tc.bobShouldSeeGuessUI {
				assertContainsAll(t, bobBody, "name=\"guess\"", ">Submit guess</button>", ">Pass</button>")
			} else {
				assertNotContainsAny(t, bobBody, "name=\"guess\"", ">Submit guess</button>", ">Pass</button>")
			}

			controllerRR := performRequest(t, app.Handler(), http.MethodGet, "/rooms/"+room.Code+"/fragment", caraToken, nil, nil)
			assertContainsAll(t, controllerRR.Body.String(), "Mark correct", "Mark wrong", "Mark pass")
		})
	}
}

func TestResolvedRoundShowsNextRoundOnlyForController(t *testing.T) {
	app := newTestApp(t)
	room := newRoundTestRoom(t, app)
	withRoomLock(t, room, func(room *Room, round *Round) {
		round.Phase = PhaseResolved
		round.TargetIndex = 1
		round.TargetWord = "Pear"
		round.Result = "correct"
		round.Clues[clueKey(caraToken, 1)] = ClueSubmission{PlayerToken: caraToken, Slot: 1, Text: "orchard"}
	})

	controllerRR := performRequest(t, app.Handler(), http.MethodGet, "/rooms/"+room.Code+"/fragment", bobToken, nil, nil)
	assertContainsAll(t, controllerRR.Body.String(), "Round result", "correct", "Next round")

	guesserRR := performRequest(t, app.Handler(), http.MethodGet, "/rooms/"+room.Code+"/fragment", aliceToken, nil, nil)
	assertContainsAll(t, guesserRR.Body.String(), "Round result", "correct")
	assertNotContainsAny(t, guesserRR.Body.String(), "Next round")
}

func TestParticipantAndSettingsSidebarRespectAdminPrivileges(t *testing.T) {
	app := newTestApp(t)
	room := newRoundTestRoom(t, app)

	adminRR := performRequest(t, app.Handler(), http.MethodGet, "/rooms/"+room.Code, aliceToken, nil, nil)
	adminBody := adminRR.Body.String()
	assertContainsAll(t, adminBody,
		"action=\"/rooms/"+room.Code+"/actions/participant-role\"",
		"action=\"/rooms/"+room.Code+"/actions/participant-admin\"",
		"action=\"/rooms/"+room.Code+"/actions/settings\"",
		"data-preserve-open=\"settings-advanced\"",
		"data-preserve-open=\"settings-packs\"",
		"action=\"/rooms/"+room.Code+"/packs/upload\"",
	)

	userRR := performRequest(t, app.Handler(), http.MethodGet, "/rooms/"+room.Code, bobToken, nil, nil)
	userBody := userRR.Body.String()
	assertContainsAll(t, userBody, "<ul class=\"summary-list\">", "<span>Word pack</span>")
	assertNotContainsAny(t, userBody,
		"action=\"/rooms/"+room.Code+"/actions/participant-role\"",
		"action=\"/rooms/"+room.Code+"/actions/participant-admin\"",
		"action=\"/rooms/"+room.Code+"/actions/settings\"",
		"data-preserve-open=\"settings-advanced\"",
		"action=\"/rooms/"+room.Code+"/packs/upload\"",
	)
}

func TestFailedBrowserActionPersistsFlashOnRoomPage(t *testing.T) {
	app := newTestApp(t)
	room := app.createRoom(aliceToken, "Alice")

	actionRR := performFormRequest(t, app.Handler(), "/rooms/"+room.Code+"/actions/start", aliceToken, "", false)
	assertStatus(t, actionRR.Code, http.StatusSeeOther)

	pageRR := performRequest(t, app.Handler(), http.MethodGet, "/rooms/"+room.Code, aliceToken, nil, nil)
	body := pageRR.Body.String()
	assertContainsAll(t, body, "flash-banner", "need at least 2 players to start")
}

func TestJoinMidRoundRedirectsAndAddsObserver(t *testing.T) {
	app := newTestApp(t)
	room := newRoundTestRoom(t, app)
	withRoomLock(t, room, func(room *Room, round *Round) {
		round.Phase = PhaseClueReview
		round.TargetWord = "Pear"
	})

	rr := performFormRequest(t, app.Handler(), "/rooms/join", daveToken, "code="+room.Code+"&name=Dave", false)
	assertStatus(t, rr.Code, http.StatusSeeOther)
	if got := rr.Header().Get("Location"); got != "/rooms/"+room.Code {
		t.Fatalf("expected redirect to room page, got %q", got)
	}

	room.mu.RLock()
	participant := room.Participants[daveToken]
	room.mu.RUnlock()
	if participant == nil || participant.Role != RoleObserver {
		t.Fatalf("expected mid-round joiner to be an observer, got %#v", participant)
	}
}

func TestRoomEventsEmitRefreshAfterStateChange(t *testing.T) {
	app := newTestApp(t)
	room := app.createRoom(aliceToken, "Alice")
	room.mu.Lock()
	room.join(bobToken, "Bob")
	room.mu.Unlock()

	server := httptest.NewServer(app.Handler())
	defer server.Close()

	resp, err := server.Client().Get(server.URL + "/rooms/" + room.Code + "/events")
	if err != nil {
		t.Fatalf("GET events returned error: %v", err)
	}
	defer resp.Body.Close()

	reader := bufio.NewReader(resp.Body)
	initial, err := readSSEEvent(reader)
	if err != nil {
		t.Fatalf("reading initial SSE event failed: %v", err)
	}
	assertContainsAll(t, initial, "event: refresh\n", "data: ready\n")

	req, err := http.NewRequest(http.MethodPost, server.URL+"/rooms/"+room.Code+"/actions/start", nil)
	if err != nil {
		t.Fatalf("creating start request failed: %v", err)
	}
	req.AddCookie(&http.Cookie{Name: "snowflakes_auth_token", Value: aliceToken})
	req.Header.Set("X-Requested-With", "fetch")
	actionResp, err := server.Client().Do(req)
	if err != nil {
		t.Fatalf("POST start returned error: %v", err)
	}
	actionResp.Body.Close()
	if actionResp.StatusCode != http.StatusNoContent {
		t.Fatalf("expected %d, got %d", http.StatusNoContent, actionResp.StatusCode)
	}

	refresh, err := readSSEEvent(reader)
	if err != nil {
		t.Fatalf("reading refresh SSE event failed: %v", err)
	}
	assertContainsAll(t, refresh, "event: refresh\n", "data:")
	if strings.Contains(refresh, "data: ready\n") {
		t.Fatalf("expected a state-change refresh, got %q", refresh)
	}
}

func readSSEEvent(reader *bufio.Reader) (string, error) {
	type result struct {
		event string
		err   error
	}
	done := make(chan result, 1)
	go func() {
		var lines []string
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				done <- result{err: err}
				return
			}
			lines = append(lines, line)
			if line == "\n" {
				done <- result{event: strings.Join(lines, "")}
				return
			}
		}
	}()

	select {
	case res := <-done:
		return res.event, res.err
	case <-time.After(2 * time.Second):
		return "", http.ErrHandlerTimeout
	}
}
