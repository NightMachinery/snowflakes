package snowflakes

import (
	"bufio"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func newTestApp(t *testing.T) *App {
	t.Helper()
	app, err := NewApp(Config{
		Host:        "127.0.0.1",
		Port:        3400,
		PublicURL:   "http://example.com",
		WordPackDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("NewApp returned error: %v", err)
	}
	return app
}

func TestIndexRendersLandingPage(t *testing.T) {
	app := newTestApp(t)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)

	app.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d", http.StatusOK, rr.Code)
	}
	body := rr.Body.String()
	for _, want := range []string{"<!doctype html>", "<main class=\"page-shell landing-shell\">", "Create room", "Join room", "keep the hidden info actually hidden"} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected body to contain %q, got %q", want, body)
		}
	}
	if cookie := rr.Result().Cookies(); len(cookie) == 0 {
		t.Fatal("expected auth cookie to be set")
	}
}

func TestRoomPageAndFragmentRender(t *testing.T) {
	app := newTestApp(t)
	room := app.createRoom("creator-token", "Alice")
	handler := app.Handler()

	pageReq := httptest.NewRequest(http.MethodGet, "/rooms/"+room.Code, nil)
	pageReq.AddCookie(&http.Cookie{Name: "snowflakes_auth_token", Value: "creator-token"})
	pageRR := httptest.NewRecorder()
	handler.ServeHTTP(pageRR, pageReq)

	if pageRR.Code != http.StatusOK {
		t.Fatalf("expected room page status %d, got %d", http.StatusOK, pageRR.Code)
	}
	pageBody := pageRR.Body.String()
	for _, want := range []string{
		"id=\"room-root\"",
		"data-room-code=\"" + room.Code + "\"",
		"You are here",
		"<strong>Alice</strong>",
		"Room " + room.Code,
		"data-copy-text=\"http://example.com/rooms/" + room.Code + "\"",
		"Copy link",
	} {
		if !strings.Contains(pageBody, want) {
			t.Fatalf("expected room page to contain %q, got %q", want, pageBody)
		}
	}

	fragmentReq := httptest.NewRequest(http.MethodGet, "/rooms/"+room.Code+"/fragment", nil)
	fragmentReq.AddCookie(&http.Cookie{Name: "snowflakes_auth_token", Value: "creator-token"})
	fragmentRR := httptest.NewRecorder()
	handler.ServeHTTP(fragmentRR, fragmentReq)

	if fragmentRR.Code != http.StatusOK {
		t.Fatalf("expected fragment status %d, got %d", http.StatusOK, fragmentRR.Code)
	}
	fragmentBody := fragmentRR.Body.String()
	if strings.Contains(fragmentBody, "<!doctype html>") {
		t.Fatalf("fragment should not contain full document, got %q", fragmentBody)
	}
	for _, want := range []string{"class=\"room-shell\"", "Room " + room.Code, "Copy link", "Ready to start"} {
		if !strings.Contains(fragmentBody, want) {
			t.Fatalf("expected fragment to contain %q, got %q", want, fragmentBody)
		}
	}
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

func TestStaticAssetServed(t *testing.T) {
	app := newTestApp(t)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/static/app.js", nil)

	app.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d", http.StatusOK, rr.Code)
	}
	body := rr.Body.String()
	for _, want := range []string{"snowflakesRefreshRoom", "snowflakes_player_name", "execCommand('copy')", "Copy this room link:", "data-preserve-open"} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected JS asset body to contain %q, got %q", want, body)
		}
	}
}

func TestLightThemeStylesheetServed(t *testing.T) {
	app := newTestApp(t)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/static/styles.css", nil)

	app.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d", http.StatusOK, rr.Code)
	}
	body := rr.Body.String()
	for _, want := range []string{"color-scheme: light;", ".room-layout", ".settings-details"} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected CSS asset body to contain %q, got %q", want, body)
		}
	}
	if strings.Contains(body, "color-scheme: dark") {
		t.Fatalf("did not expect dark color-scheme in CSS, got %q", body)
	}
}

func TestStartActionReturnsAndStartsRound(t *testing.T) {
	app := newTestApp(t)
	room := app.createRoom("creator-token", "Alice")
	room.mu.Lock()
	room.join("bob-token", "Bob")
	room.mu.Unlock()

	req := httptest.NewRequest(http.MethodPost, "/rooms/"+room.Code+"/actions/start", nil)
	req.AddCookie(&http.Cookie{Name: "snowflakes_auth_token", Value: "creator-token"})
	req.Header.Set("X-Requested-With", "fetch")
	rr := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		app.Handler().ServeHTTP(rr, req)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("start action timed out")
	}

	if rr.Code != http.StatusNoContent {
		t.Fatalf("expected %d, got %d", http.StatusNoContent, rr.Code)
	}
	room.mu.RLock()
	defer room.mu.RUnlock()
	if room.Game == nil || room.Game.Status != GameRunning || room.Game.CurrentRound == nil {
		t.Fatalf("expected running game with active round, got %#v", room.Game)
	}
}

func TestBlindSlotGuesserFragmentHidesChoiceWords(t *testing.T) {
	app := newTestApp(t)
	room := app.createRoom("creator-token", "Alice")
	room.mu.Lock()
	room.join("bob-token", "Bob")
	room.Game = &Game{
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
	}
	room.assignTemporaryRoundController(room.Game.CurrentRound)
	room.mu.Unlock()

	guesserReq := httptest.NewRequest(http.MethodGet, "/rooms/"+room.Code+"/fragment", nil)
	guesserReq.AddCookie(&http.Cookie{Name: "snowflakes_auth_token", Value: "creator-token"})
	guesserRR := httptest.NewRecorder()
	app.Handler().ServeHTTP(guesserRR, guesserReq)
	guesserBody := guesserRR.Body.String()
	if !strings.Contains(guesserBody, "Hidden word") {
		t.Fatalf("expected guesser view to hide selection words, got %q", guesserBody)
	}
	if strings.Contains(guesserBody, "Apple") || strings.Contains(guesserBody, "Pear") || strings.Contains(guesserBody, "Peach") {
		t.Fatalf("did not expect guesser view to contain choice words, got %q", guesserBody)
	}

	cluegiverReq := httptest.NewRequest(http.MethodGet, "/rooms/"+room.Code+"/fragment", nil)
	cluegiverReq.AddCookie(&http.Cookie{Name: "snowflakes_auth_token", Value: "bob-token"})
	cluegiverRR := httptest.NewRecorder()
	app.Handler().ServeHTTP(cluegiverRR, cluegiverReq)
	cluegiverBody := cluegiverRR.Body.String()
	if !strings.Contains(cluegiverBody, "Apple") || !strings.Contains(cluegiverBody, "Pear") {
		t.Fatalf("expected clue-giver view to contain choice words, got %q", cluegiverBody)
	}
}
