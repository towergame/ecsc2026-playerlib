package playerlib

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"
)

func TestNormalizeBaseURL(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "normalized url is unchanged",
			in:   "https://h/api/",
			want: "https://h/api/",
		},
		{
			name: "missing slash is added",
			in:   "https://h/api",
			want: "https://h/api/",
		},
		{
			name: "filename is stripped",
			in:   "https://h/api/attack.json",
			want: "https://h/api/",
		},
		{
			name: "different filename is stripped",
			in:   "https://h/api/scoreboard_current.json",
			want: "https://h/api/",
		},
		{
			name: "appends API",
			in:   "https://h",
			want: "https://h/api/",
		},
		{
			name: "appends API with trailing slash",
			in:   "https://h/",
			want: "https://h/api/",
		},
		{
			name: "appends API with port",
			in:   "https://h:4200",
			want: "https://h:4200/api/",
		},
		{
			name: "appends API with credentials",
			in:   "https://u:p@h",
			want: "https://u:p@h/api/",
		},
		{
			name: "appends API to path",
			in:   "https://h/game",
			want: "https://h/game/api/",
		},
		{
			name: "filename remains at the same directory",
			in:   "https://h/attack.json",
			want: "https://h/",
		},
		{
			name: "assumes HTTP scheme",
			in:   "h.de",
			want: "http://h.de/api/",
		},
		{
			name: "assumes HTTP scheme with API",
			in:   "h.de/api",
			want: "http://h.de/api/",
		},
		{
			name: "assumes HTTP scheme with port",
			in:   "1.2.3.4:4200",
			want: "http://1.2.3.4:4200/api/",
		},
		{
			name: "assumes HTTP scheme with file",
			in:   "1.2.3.4/api/attack.json",
			want: "http://1.2.3.4/api/",
		},
		{
			name: "localhost works",
			in:   "localhost",
			want: "http://localhost/api/",
		},
		{
			name: "localhost with port works",
			in:   "localhost:4200",
			want: "http://localhost:4200/api/",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeBaseURL(tt.in)
			if got != tt.want {
				t.Errorf("normalizeBaseURL(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestDefaultURL(t *testing.T) {
	t.Run("default URL is the ECSC2026 scoreboard", func(t *testing.T) {
		t.Setenv(ENV_VAR, "")

		api, err := NewECSCApi(nil, nil, nil, nil, false)

		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		if api.baseURL != "https://scoreboard.ad.ecsc2026.de/api/" {
			t.Errorf("Expected default URL to be https://scoreboard.ad.ecsc2026.de/api/, got %s", api.baseURL)
		}
	})

	t.Run("environment variable overrides default URL", func(t *testing.T) {
		t.Setenv(ENV_VAR, "10.13.37.1:4200")

		api, err := NewECSCApi(nil, nil, nil, nil, false)

		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		if api.baseURL != "http://10.13.37.1:4200/api/" {
			t.Errorf("Expected URL to be http://10.13.37.1:4200/api/, got %s", api.baseURL)
		}

	})

	t.Run("explicit URL overrides environment variable", func(t *testing.T) {
		t.Setenv(ENV_VAR, "10.13.37.1:4200")

		explicitURL := "https://h"
		api, err := NewECSCApi(&explicitURL, nil, nil, nil, false)

		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		if api.baseURL != "https://h/api/" {
			t.Errorf("Expected URL to be https://h/api/, got %s", api.baseURL)
		}
	})
}

func newTestApi(t *testing.T, serverURL string) *ECSCApi {
	t.Helper()

	cacheDir := t.TempDir()

	api, err := NewECSCApi(&serverURL, &cacheDir, nil, nil, false) // set sharedCache to false to avoid interfering with other tests

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	return api
}

type testServer struct {
	*httptest.Server

	mu   sync.Mutex
	urls []string
}

func newTestServer(t *testing.T) *testServer {
	t.Helper()

	ts := &testServer{}

	ts.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ts.mu.Lock()
		ts.urls = append(ts.urls, r.URL.Path)
		ts.mu.Unlock()

		name := path.Base(r.URL.Path)

		data, err := os.ReadFile(filepath.Join("testdata", name))
		if err != nil {
			http.NotFound(w, r)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(data)
	}))

	return ts
}

func (ts *testServer) URLs() []string {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	return append([]string(nil), ts.urls...)
}

func TestAPI(t *testing.T) {
	t.Run("test validation", func(t *testing.T) {
		t.Run("lifetime must be at least 1 second", func(t *testing.T) {
			lifetime := 500 * time.Millisecond
			_, err := NewECSCApi(nil, nil, &lifetime, nil, false)
			if err == nil {
				t.Fatal("Expected error for lifetime less than 1 second, got nil")
			}
		})

		t.Run("lifetime must be greater than or equal to timeout", func(t *testing.T) {
			lifetime := 5 * time.Second
			timeout := 10 * time.Second
			_, err := NewECSCApi(nil, nil, &lifetime, &timeout, false)
			if err == nil {
				t.Fatal("Expected error for lifetime less than timeout, got nil")
			}
		})
	})

	t.Run("test fetching attack info", func(t *testing.T) {
		server := newTestServer(t)
		defer server.Close()

		api := newTestApi(t, server.URL)

		attackInfo, err := api.GetAttackInfo(context.Background())
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		if attackInfo == nil {
			t.Fatal("Expected attack info, got nil")
		}

		res, err := attackInfo.GetFlagIDs("ServiceA", "nop", nil)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		if !slices.Contains(res, "alice") {
			t.Errorf("Expected 'alice' in flag IDs, results: %v", res)
		}
	})

	t.Run("test fetching scoreboard", func(t *testing.T) {
		server := newTestServer(t)
		defer server.Close()

		api := newTestApi(t, server.URL)

		scoreboard, err := api.GetScoreboard(context.Background(), nil)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		if scoreboard == nil {
			t.Fatal("Expected scoreboard, got nil")
		}

		if scoreboard.Round != 6 {
			t.Errorf("Expected round 6, got %d", scoreboard.Round)
		}

		urls := server.URLs()

		if !slices.Contains(urls, "/api/scoreboard_current.json") {
			t.Errorf("Expected /api/scoreboard_current.json to be requested, URLs: %v", urls)
		}

		if !slices.Contains(urls, "/api/scoreboard_round_6.json") {
			t.Errorf("Expected /api/scoreboard_round_6.json to be requested, URLs: %v", urls)
		}
	})

	t.Run("test all endpoints", func(t *testing.T) {
		server := newTestServer(t)
		defer server.Close()

		api := newTestApi(t, server.URL)

		// Game state
		t.Run("test game state", func(t *testing.T) {
			gameState, err := api.GetGameState(context.Background())
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			if gameState == nil {
				t.Fatal("Expected game state, got nil")
			}

			if gameState.State != Running {
				t.Errorf("Expected state 'running', got %d", gameState.State)
			}
		})

		// ScoreboardTeams
		t.Run("test scoreboard teams", func(t *testing.T) {
			scoreboardTeams, err := api.GetScoreboardTeams(context.Background())
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			if scoreboardTeams == nil {
				t.Fatal("Expected scoreboard teams, got nil")
			}

			if len(*scoreboardTeams) != 2 {
				t.Errorf("Expected 2 teams, got %d", len(*scoreboardTeams))
			}

			if !(*scoreboardTeams)["2"].Logo.Defined || *(*scoreboardTeams)["2"].Logo.Value != "def456.png" {
				t.Errorf("Expected team 2 logo 'def456.png', got %s", *(*scoreboardTeams)["2"].Logo.Value)
			}
		})

		// ServiceStats
		t.Run("test service stats", func(t *testing.T) {
			serviceStats, err := api.GetServiceStats(context.Background())
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			if serviceStats == nil {
				t.Fatal("Expected service stats, got nil")
			}

			if !slices.Equal(serviceStats.Services, []string{"ServiceA", "ServiceB"}) {
				t.Errorf("Expected services ['ServiceA', 'ServiceB'], got %v", serviceStats.Services)
			}
		})

		// TeamHistory
		t.Run("test team history", func(t *testing.T) {
			teamHistory, err := api.GetTeamHistory(context.Background(), 2)
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			if teamHistory == nil {
				t.Fatal("Expected team history, got nil")
			}

			if !slices.Equal(teamHistory.GetTotalPoints(), []float32{0.0, 40.0}) {
				t.Errorf("Expected total points [0.0, 40.0], got %v", teamHistory.GetTotalPoints())
			}
		})

	})

	t.Run("test memory cache", func(t *testing.T) {
		server := newTestServer(t)
		defer server.Close()

		testDir := t.TempDir()

		api, err := NewECSCApi(&server.URL, &testDir, nil, nil, true)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		api.GetAttackInfo(context.Background())

		api2, err := NewECSCApi(&server.URL, &testDir, nil, nil, true)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		api2.GetAttackInfo(context.Background())

		urls := server.URLs()
		if len(urls) != 1 {
			t.Errorf("Expected 1 requests to server, got %d", len(urls))
		}
	})

	t.Run("test file cache", func(t *testing.T) {
		server := newTestServer(t)
		defer server.Close()

		testDir := t.TempDir()

		api, err := NewECSCApi(&server.URL, &testDir, nil, nil, false)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		api.GetAttackInfo(context.Background())

		api2, err := NewECSCApi(&server.URL, &testDir, nil, nil, false)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		api2.GetAttackInfo(context.Background())

		urls := server.URLs()
		if len(urls) != 1 {
			t.Errorf("Expected 1 request to server, got %d", len(urls))
		}
	})

	t.Run("test cache expiration", func(t *testing.T) {
		server := newTestServer(t)
		defer server.Close()

		interval := time.Second * 2
		timeout := time.Second
		api, err := NewECSCApi(&server.URL, nil, &interval, &timeout, false)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		api.GetAttackInfo(context.Background())

		time.Sleep(time.Second * 3) // this makes testing long but I cbb to refactor things

		api.GetAttackInfo(context.Background())

		urls := server.URLs()
		if len(urls) != 2 {
			t.Errorf("Expected 2 requests, got %d", len(urls))
		}
	})
}
