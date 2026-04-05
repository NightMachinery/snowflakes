package snowflakes

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const (
	aliceToken = "alice-token"
	bobToken   = "bob-token"
	caraToken  = "cara-token"
	daveToken  = "dave-token"
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

func newRoundTestRoom(t *testing.T, app *App) *Room {
	t.Helper()
	room := app.createRoom(aliceToken, "Alice")
	room.mu.Lock()
	defer room.mu.Unlock()

	room.join(bobToken, "Bob")
	room.join(caraToken, "Cara")
	room.join(daveToken, "Dave")
	room.Participants[daveToken].Role = RoleObserver
	room.Game = &Game{
		Status:       GameRunning,
		CurrentIndex: 0,
		Deck:         []RoundCard{testRoundCard(), testRoundCard()},
		CurrentRound: testRound(),
	}
	room.assignTemporaryRoundController(room.Game.CurrentRound)
	return room
}

func testRoundCard() RoundCard {
	return RoundCard{
		Pool:  []string{"Apple", "Pear", "Peach", "Plum", "Lime"},
		Slate: []string{"Apple", "Pear", "Peach"},
	}
}

func testRound() *Round {
	return &Round{
		Phase:         PhaseWordSelection,
		Card:          testRoundCard(),
		VotesByToken:  map[string]int{},
		Clues:         map[string]ClueSubmission{},
		ManualInvalid: map[string]bool{},
		Guesses:       map[string]string{},
		PassByToken:   map[string]bool{},
	}
}

func withRoomLock(t *testing.T, room *Room, fn func(*Room, *Round)) {
	t.Helper()
	room.mu.Lock()
	defer room.mu.Unlock()
	if room.Game == nil || room.Game.CurrentRound == nil {
		t.Fatal("room has no active round")
	}
	fn(room, room.Game.CurrentRound)
	room.assignTemporaryRoundController(room.Game.CurrentRound)
}

func performRequest(t *testing.T, handler http.Handler, method, path, token string, body io.Reader, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, body)
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	if token != "" {
		req.AddCookie(&http.Cookie{Name: "snowflakes_auth_token", Value: token})
	}
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	return rr
}

func performFormRequest(t *testing.T, handler http.Handler, path, token, form string, ajax bool) *httptest.ResponseRecorder {
	t.Helper()
	headers := map[string]string{"Content-Type": "application/x-www-form-urlencoded"}
	if ajax {
		headers["X-Requested-With"] = "fetch"
	}
	return performRequest(t, handler, http.MethodPost, path, token, strings.NewReader(form), headers)
}

func assertStatus(t *testing.T, got, want int) {
	t.Helper()
	if got != want {
		t.Fatalf("expected status %d, got %d", want, got)
	}
}

func assertContainsAll(t *testing.T, body string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(body, want) {
			t.Fatalf("expected body to contain %q, got %q", want, body)
		}
	}
}

func assertNotContainsAny(t *testing.T, body string, unwants ...string) {
	t.Helper()
	for _, unwanted := range unwants {
		if strings.Contains(body, unwanted) {
			t.Fatalf("did not expect body to contain %q, got %q", unwanted, body)
		}
	}
}

func assertContainsCount(t *testing.T, body, needle string, want int) {
	t.Helper()
	if got := strings.Count(body, needle); got != want {
		t.Fatalf("expected %q to appear %d times, got %d in %q", needle, want, got, body)
	}
}
