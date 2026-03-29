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
	for _, want := range []string{"<!doctype html>", "<main class=\"landing\">", "Create room", "Join room"} {
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
		"You are <strong>Alice</strong>",
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
	for _, want := range []string{"class=\"room-shell\"", "Room " + room.Code, "Copy link", "No active round yet."} {
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
	for _, want := range []string{"snowflakesRefreshRoom", "snowflakes_player_name", "execCommand('copy')", "Copy this room link:"} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected JS asset body to contain %q, got %q", want, body)
		}
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
