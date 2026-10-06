package main

import "encoding/json"

// GameState represents the current state of the game.
type GameState int

const (
	Stopped GameState = iota + 1
	Paused
	Running
)

// CheckerStatus of a service for one team and round: the gameserver's verdict over all checker tasks of that round, derived from the CheckerTaskResult values (OK / MUMBLE / OFFLINE / INTERNAL_ERROR) the checker returned for each of them.
//   - SUCCESS: every task returned OK
//   - RECOVERING: this round's tasks are fine, but a task for an earlier round did not return OK - the service works, old flags are not retrievable
//   - MUMBLE: a task of this round returned MUMBLE
//   - OFFLINE: a task of this round returned OFFLINE
//   - TIMEOUT: a task did not answer within its timeout (INTERNAL_ERROR, "TIMEOUT")
//   - CRASHED: a task failed with INTERNAL_ERROR for any other reason
//   - REVOKED: the one value that never comes from a checker - no result exists, because the task was cancelled when the round ended or answered too late. A task still in flight when its round is collected is revoked as well, so a round's statuses are final once it is scored.
type CheckerStatus string

const (
	Success    CheckerStatus = "SUCCESS"
	Recovering CheckerStatus = "RECOVERING"
	Mumble     CheckerStatus = "MUMBLE"
	Offline    CheckerStatus = "OFFLINE"
	Timeout    CheckerStatus = "TIMEOUT"
	Crashed    CheckerStatus = "CRASHED"
	Revoked    CheckerStatus = "REVOKED"
)

// ScoreboardCurrent represents the API result for /api/scoreboard_current.json
type ScoreboardCurrent struct {
	// CurrentRound is the round we're currently playing (if live), 0 before the game, last played round after the game.
	CurrentRound int `json:"current_round"`
	// State is the current state of the game (Stopped, Paused, Running).
	State GameState `json:"state"`
	// ValidityPeriod is the number of rounds that the flags are valid for (in addition to the round they're stored in)
	ValidityPeriod int `json:"validity_period"`
	// CurrentRoundStart is the Unix timestamp when the current round started.
	CurrentRoundStart *int64 `json:"current_round_start,omitempty"`
	// CurrentRoundUntil is the Unix timestamp when the current round will end.
	CurrentRoundUntil *int64 `json:"current_round_until,omitempty"`
	// ScoreboardRound is round number until the scoreboard data is created (per-round urls are available).
	// -1 is a valid round where the game hasn't started yet and the service names are hidden.
	ScoreboardRound int `json:"scoreboard_round,omitempty"`
	// BannedTeams is a list of team IDs that are cut off from the network, or permanently banned.
	BannedTeams []int `json:"banned_teams,omitempty"`
	// Frozen is true if the scoreboard is frozen (scores not updated in the final moments of the game).
	Frozen bool `json:"frozen,omitempty"`
}

// TeamServiceRoundInfo represents scores for one service for one team (one cell in the main scoreboard table).
type TeamServiceRoundInfo struct {
	// OffensePoints represents the offense points, total.
	OffensePoints float32 `json:"o"`
	// DefensePoints represents the defense points, total.
	DefensePoints float32 `json:"d"`
	// SLAPoints represents the SLA points, total.
	SLAPoints float32 `json:"s"`

	// OffensePointsDelta represents the difference in offense points compared to previous round.
	OffensePointsDelta float32 `json:"do"`
	// DefensePointsDelta represents the difference in defense points compared to previous round.
	DefensePointsDelta float32 `json:"dd"`
	// SLAPointsDelta represents the difference in SLA points compared to previous round.
	SLAPointsDelta float32 `json:"ds"`

	// StolenFlags is the number of flags stolen from this team (this team is the victim).
	StolenFlags int `json:"st"`
	// CapturedFlags is the number of flags captured from other teams (this team is the attacker).
	CapturedFlags int `json:"cap"`

	// StolenFlagsDelta is the difference in stolen flags compared to previous round.
	StolenFlagsDelta int `json:"dst"`
	// CapturedFlagsDelta is the difference in captured flags compared to previous round.
	CapturedFlagsDelta int `json:"dcap"`

	// CheckerStatus is the checker status in this round.
	CheckerStatus CheckerStatus `json:"c"`
	// CheckerMessage is the message from the checker in this round (if any). Can be empty or null if missing.
	CheckerMessage *string `json:"m,omitempty"`
	// CheckerStatusHistory is the checker status in the past N rounds. Order: newest first.
	CheckerStatusHistory []CheckerStatus `json:"ch,omitempty"`
}

// ScoreboardTeamRow represents the per-round stats of one team (single row in the scoreboard table).
type ScoreboardTeamRow struct {
	// TeamID is the ID of the team.
	TeamID int `json:"team_id"`
	// Rank is the current rank of the team (1 is the winning team, ranks might or might not be unique if teams have the same score).
	Rank int `json:"rank"`
	// Points is the total points of the team.
	Points float32 `json:"points"`
	// Services is the scores of each service for this team.
	Services []TeamServiceRoundInfo `json:"services"`

	// OffensePoints is the total offense points of the team.
	OffensePoints float32 `json:"o"`
	// DefensePoints is the total defense points of the team.
	DefensePoints float32 `json:"d"`
	// SLAPoints is the total SLA points of the team.
	SLAPoints float32 `json:"s"`

	// OffensePointsDelta is the difference in offense points compared to previous round.
	OffensePointsDelta float32 `json:"do"`
	// DefensePointsDelta is the difference in defense points compared to previous round.
	DefensePointsDelta float32 `json:"dd"`
	// SLAPointsDelta is the difference in SLA points compared to previous round.
	SLAPointsDelta float32 `json:"ds"`
}

// FirstBlood is the information of the first team to exploit the flagstore of a given service.
type FirstBlood struct {
	// Name is the name of the team that achieved the first blood.
	Name *string `json:"name,omitempty"`
	// Ts is the Unix timestamp when the first blood was achieved.
	Ts *int64 `json:"ts,omitempty"`

	// Confirmed is true if the first blood is considered valid (if relevant for game definition).
	Confirmed *bool `json:"confirmed,omitempty"`
	// Level is the firstblood level achieved (if supported by game definition).
	Level *int `json:"level,omitempty"`
	// Payload is the payload / flag store index this first blood was scored on.
	Payload *int `json:"payload,omitempty"`
}

// HiddenInt is a union of int and "?", where int is defined if the value is not hidden.
type HiddenInt struct {
	// Value is the integer value, if not hidden.
	Value *int
	// Hidden is true if the value is hidden (represented as "?").
	Hidden bool
}

func (hi *HiddenInt) UnmarshalJSON(data []byte) error {
	if string(data) == `"?"` {
		hi.Hidden = true
		hi.Value = nil
		return nil
	}

	return json.Unmarshal(data, &hi.Value)
}

func (hi HiddenInt) MarshalJSON() ([]byte, error) {
	if hi.Hidden {
		return []byte(`"?"`), nil
	}

	return json.Marshal(hi.Value)
}

// ServiceRoundInfo is the status of one service for one round (in scoreboard table: header cell with service name).
type ServiceRoundInfo struct {
	// Name is the name of the service.
	Name string `json:"name"`
	// Attackers is the number of teams that successfully stole at least one flag in this round, or '?' if hidden.
	Attackers HiddenInt `json:"attackers"`
	// Victims is the number of teams that had at least one flag stolen in this round, or '?' if hidden.
	Victims HiddenInt `json:"victims"`
	// FirstBlood is the list of teams that achieved first blood on a flag store of this service. One entry per first-blooded flagstore.
	FirstBlood []FirstBlood `json:"first_blood,omitempty"`
	// FlagStores is the total number of flag stores for this service.
	FlagStores int `json:"flag_stores"`
	// FlagStoresExploited is the number of flag stores that have already been first-blooded.
	FlagStoresExploited int `json:"flag_stores_exploited"`
}

// ScoreboardRound is the API result for /api/scoreboard_round_{round}.json
type ScoreboardRound struct {
	// Round is the round number (as in request).
	// Data for round 1 is created AFTER round 1 finished (while round 2 is running).
	// Round -1 is before the game starts (service names hidden).
	// Round 0 is when the game starts but no round has completed (service names revealed).
	Round int `json:"round"`
	// Scoreboard is the list of all teams, ordered by score descending (like the scoreboard table)
	Scoreboard []ScoreboardTeamRow `json:"scoreboard"`
	// Services is the list of all played services
	Services []ServiceRoundInfo `json:"services"`
}

// ScoreboardTeamHistory is the API result for /api/scoreboard_team_{team_id}.json
type ScoreboardTeamHistory struct {
	// Services is the ordered list of all services.
	Services []string `json:"services"`
	// Points is the list of points per service, items in same order as services (points[i][round] belongs to services[i])
	Points [][]float32 `json:"points"`
}

// StaticTeamInfo is the static information about one team.
type StaticTeamInfo struct {
	// Name is the name of the team.
	Name string `json:"name"`
	// Vulnbox is the IP address of the team's vulnbox
	Vulnbox string `json:"vulnbox"`
	// Affiliation is the team affiliation or organization
	Affiliation string `json:"aff"`
	// Website is the team website URL (if given)
	Website *string `json:"website,omitempty"`
	// Logo is the filename of the team's logo image, without path or url.
	// Usually you can fetch it from /logos/<filename> (NOT /api/logos/...).
	// If the logo is not set, this might be the filename of a placeholder image, or false.
	Logo *string `json:"logo,omitempty"`
}

// ScoreboardTeams is the information about all teams, indexed by team ID.
// API result for /api/scoreboard_teams.json
type ScoreboardTeams struct {
	// Root is the root object, mapping team IDs to StaticTeamInfo.
	Root map[string]StaticTeamInfo `json:"root"`
}

// ServiceAttackerVictimCount is the number of attackers/victims in one round and service.
type ServiceAttackerVictimCount struct {
	// NumAttackers is the number of attacking teams in this round and service.
	NumAttackers HiddenInt `json:"a"`
	// NumVictims is the number of victim teams in this round and service.
	NumVictims HiddenInt `json:"v"`
}

// ScoreboardServiceStats is the number of attackers/victims per service over time.
// API result for /api/scoreboard_service_stats.json
type ScoreboardServiceStats struct {
	// Services is the list of all services we have stats for.
	Services []string `json:"services"`
	// Stats is the lst of attacker/victim timeline per service, same length and order as services.
	// stats[i][round] belongs to services[i].
	Stats [][]ServiceAttackerVictimCount `json:"stats"`
}

// AttackInfoTeam represents one attackable team.
type AttackInfoTeam struct {
	// ID is the ID of the team.
	ID int `json:"id"`
	// Name is the name of the team.
	Name string `json:"name"`
	// IP is the IP address of the team's vulnbox.
	IP string `json:"ip"`
}

// AttackInfo represents the flag format, teams and attack info for the current round.
// API result for /api/attack.json
type AttackInfo struct {
	// FlagRegex is the regular expression that matches a flag, to find flags in exploit output
	FlagRegex string `json:"flag_regex"`
	// Teams is the list of all teams that can be attacked.
	Teams []AttackInfoTeam `json:"teams"`
	// AttackInfo is the attack info, keyed by service name, then by vulnbox IP, then by round.
	// Only services that publish attack info are listed.
	// Rounds range over the still-valid ones, excluding the current round -
	// its attack info is published once the round is over.
	AttackInfo map[string]map[string]map[string]*string `json:"attack_info"`
	// CurrentRound is the round number we're currently playing (if live), 0 before the game, last played round after the game.
	CurrentRound int `json:"current_round"`
	// CurrentRoundStart is the Unix timestamp when the current round started.
	CurrentRoundStart *int64 `json:"current_round_start,omitempty"`
	// CurrentRoundUntil is the Unix timestamp when the current round will end.
	CurrentRoundUntil *int64 `json:"current_round_until,omitempty"`
}
