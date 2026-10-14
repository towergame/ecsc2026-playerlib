// This file contains the ECSC2026 API client.
// It provides functions to interact with the ECSC2026 API, alongside caching in memory+disk.
package playerlib

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"runtime/debug"
	"strings"
	"time"

	"golang.org/x/sync/singleflight"
)

const ENV_VAR = "ECSC_API"
const DEFAULT_API_URL = "https://scoreboard.ad.ecsc2026.de"

func assumeScheme(url string) string {
	if strings.Contains(url, "://") {
		return url
	}

	return "http://" + url
}

func normalizeBaseURL(base_url string) string {
	base_url = assumeScheme(base_url)
	if strings.HasSuffix(base_url, ".json") {
		split := strings.Split(base_url, "/")
		base_url = strings.Join(split[:len(split)-1], "/")
		return base_url + "/"
	}
	if !strings.HasSuffix(base_url, "/") {
		base_url += "/"
	}
	if !strings.HasSuffix(base_url, "api/") {
		base_url += "api/"
	}
	return base_url
}

func version() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "unknown"
	}

	return info.Main.Version
}

// ECSCApi is the main class exposing functions for interacting with the ECSC2026 API.
type ECSCApi struct {
	baseURL  string
	cacheDir string
	lifetime time.Duration
	timeout  time.Duration

	UserAgent  string
	httpClient *http.Client

	memoryCache  *MemoryCache
	singleFlight singleflight.Group
}

var memoryCache = NewMemoryCache(30 * time.Second)

func NewECSCApi(baseURL *string, cacheDir *string, lifetime *time.Duration, timeout *time.Duration, sharedCache bool) (*ECSCApi, error) {
	if baseURL == nil {
		envBaseURL := os.Getenv(ENV_VAR)
		if envBaseURL == "" {
			envBaseURL = DEFAULT_API_URL
		}
		baseURL = &envBaseURL
	}
	normalizedURL := normalizeBaseURL(*baseURL)

	if cacheDir == nil {
		// Get a temp directory for caching
		tempDir := os.TempDir()
		cacheDir = &tempDir
	}

	if lifetime == nil {
		defaultLifetime := 30 * time.Second
		lifetime = &defaultLifetime
	}

	if *lifetime < 1*time.Second {
		return nil, fmt.Errorf("lifetime must be at least 1 second")
	}

	if timeout == nil {
		defaultTimeout := 10 * time.Second
		timeout = &defaultTimeout
	}

	if *lifetime < *timeout {
		return nil, fmt.Errorf("lifetime must be greater than or equal to timeout")
	}

	var usedCache *MemoryCache
	if sharedCache {
		// This does mean lifetime is ignored, but who cares ngl
		usedCache = memoryCache
	} else {
		usedCache = NewMemoryCache(*lifetime)
	}

	return &ECSCApi{
		baseURL:      normalizedURL,
		cacheDir:     *cacheDir,
		lifetime:     *lifetime,
		timeout:      *timeout,
		UserAgent:    fmt.Sprintf("go/ecsc2026ad %s", version()),
		httpClient:   &http.Client{},
		memoryCache:  usedCache,
		singleFlight: singleflight.Group{},
	}, nil
}

// get is a generic function that performs a GET request against a given endpoint and decodes the JSON into struct T.
func get[T any](ctx context.Context, api *ECSCApi, path string) (*T, error) {
	// try memory cache first
	cacheKey := path
	if data, found := api.memoryCache.Get(cacheKey); found {
		var result T
		if err := json.Unmarshal(data, &result); err == nil {
			return &result, nil
		}
	}

	// then try file cache
	cacheFilePath := fmt.Sprintf("%s/%s", api.cacheDir, path)
	fileCache := NewFileCache(cacheFilePath)
	age, err := fileCache.Age()
	if err != nil {
		return nil, err
	}

	if age < api.lifetime.Seconds() && age >= 0 {
		data, err := fileCache.Get()
		if err == nil && data != nil {
			var result T
			if err := json.Unmarshal(data, &result); err == nil {
				// store in memory cache for faster access next time
				api.memoryCache.Set(cacheKey, data)
				return &result, nil
			}
		}
	}

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		api.baseURL+path,
		nil,
	)
	if err != nil {
		return nil, err
	}

	req.Header.Set("User-Agent", api.UserAgent)

	resp, err := api.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var result T
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	// store in file cache
	data, err := json.Marshal(result)
	if err == nil {
		_ = fileCache.Set(data)
	}
	// store in memory cache
	api.memoryCache.Set(cacheKey, data)

	return &result, nil
}

// GetAttackInfo gets attack info (teams, services, flag IDs) from `attack.json`
func (api *ECSCApi) GetAttackInfo(ctx context.Context) (*AttackInfo, error) {
	result, err, _ := api.singleFlight.Do("attack_info", func() (any, error) {
		return get[AttackInfo](ctx, api, "attack.json")
	})

	if result != nil {
		if typedResult, ok := result.(*AttackInfo); ok {
			return typedResult, err
		}
	}
	return nil, err
}

// GetGameState gets current game state (round, running/stopped, freeze) from `scoreboard_current.json`
func (api *ECSCApi) GetGameState(ctx context.Context) (*ScoreboardCurrent, error) {
	result, err, _ := api.singleFlight.Do("game_state", func() (any, error) {
		return get[ScoreboardCurrent](ctx, api, "scoreboard_current.json")
	})

	if result != nil {
		if typedResult, ok := result.(*ScoreboardCurrent); ok {
			return typedResult, err
		}
	}
	return nil, err
}

// GetScoreboard gets the scoreboard from `scoreboard_round_$round.json` for the given round.
// If no round is given, it will fetch the latest scoreboard round from the GameState.
func (api *ECSCApi) GetScoreboard(ctx context.Context, round *int) (*ScoreboardRound, error) {
	if round == nil {
		gameState, err := api.GetGameState(ctx)
		if err != nil {
			return nil, err
		}
		round = &gameState.ScoreboardRound
	}
	identifier := fmt.Sprintf("scoreboard_round_%d", *round)

	result, err, _ := api.singleFlight.Do(identifier, func() (any, error) {
		return get[ScoreboardRound](ctx, api, fmt.Sprintf("scoreboard_round_%d.json", *round))
	})

	if result != nil {
		if scoreboardRound, ok := result.(*ScoreboardRound); ok {
			return scoreboardRound, err
		}
	}
	return nil, err
}

// GetScoreboardTeams gets the scoreboard team metadata from `scoreboard_teams.json`
func (api *ECSCApi) GetScoreboardTeams(ctx context.Context) (*ScoreboardTeams, error) {
	result, err, _ := api.singleFlight.Do("scoreboard_teams", func() (any, error) {
		return get[ScoreboardTeams](ctx, api, "scoreboard_teams.json")
	})

	if result != nil {
		if typedResult, ok := result.(*ScoreboardTeams); ok {
			return typedResult, err
		}
	}
	return nil, err
}

// GetTeamHistory gets the team history from `team_history.json`
func (api *ECSCApi) GetTeamHistory(ctx context.Context, team_id int) (*ScoreboardTeamHistory, error) {
	identifier := fmt.Sprintf("scoreboard_team_%d", team_id)

	result, err, _ := api.singleFlight.Do(identifier, func() (any, error) {
		return get[ScoreboardTeamHistory](ctx, api, fmt.Sprintf("scoreboard_team_%d.json", team_id))
	})

	if result != nil {
		if typedResult, ok := result.(*ScoreboardTeamHistory); ok {
			return typedResult, err
		}
	}
	return nil, err
}

// GetServiceStats gets the attacker/victim counts over time from `scoreboard_service_stats.json`
func (api *ECSCApi) GetServiceStats(ctx context.Context) (*ScoreboardServiceStats, error) {
	result, err, _ := api.singleFlight.Do("scoreboard_service_stats", func() (any, error) {
		return get[ScoreboardServiceStats](ctx, api, "scoreboard_service_stats.json")
	})

	if result != nil {
		if typedResult, ok := result.(*ScoreboardServiceStats); ok {
			return typedResult, err
		}
	}
	return nil, err
}
