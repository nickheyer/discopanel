package autopilot

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/nickheyer/discopanel/pkg/runtimespec"
)

// Durable record of the doctor's actions on one server
type doctorState struct {
	Version  int             `json:"version"`
	Incident *doctorIncident `json:"incident,omitempty"`
	Resolved *doctorIncident `json:"resolved,omitempty"`
}

// One repair campaign, opened on crash and closed on outcome
type doctorIncident struct {
	OpenedAt time.Time      `json:"opened_at"`
	ClosedAt time.Time      `json:"closed_at,omitzero"`
	Passes   int            `json:"passes"`
	Budget   int            `json:"budget"`
	Actions  []doctorAction `json:"actions"`
	Tried    []string       `json:"tried,omitempty"`
	Outcome  string         `json:"outcome,omitempty"`
	Summary  string         `json:"summary,omitempty"`
}

const (
	actionDisable     = "disable"
	actionEnable      = "enable"
	actionInstall     = "install"
	actionDisablePack = "disable_pack"
)

// Evidence grades ordered strongest first
const (
	evidenceVerdict  = "verdict"
	evidenceSolver   = "solver"
	evidenceRegistry = "registry"
	evidenceFrame    = "frame"
)

// One reversible repair step
type doctorAction struct {
	Kind      string    `json:"kind"`
	File      string    `json:"file"`
	ModID     string    `json:"mod_id,omitempty"`
	Reason    string    `json:"reason,omitempty"`
	Range     string    `json:"range,omitempty"`   // Version range for installs
	Dialect   string    `json:"dialect,omitempty"` // Metadata dialect for installs
	Evidence  string    `json:"evidence"`
	AppliedAt time.Time `json:"applied_at"`
	Reverted  bool      `json:"reverted,omitempty"`
}

// Installs key on mod id until file sourced
func (a *doctorAction) key() string {
	if a.Kind == actionInstall {
		return a.Kind + ":" + a.ModID
	}
	return a.Kind + ":" + a.File
}

func (inc *doctorIncident) tried(key string) bool {
	return slices.Contains(inc.Tried, key)
}

func (inc *doctorIncident) markTried(key string) {
	if !inc.tried(key) {
		inc.Tried = append(inc.Tried, key)
	}
}

// Counts live disables, the budget consumers
func (inc *doctorIncident) disabledCount() int {
	n := 0
	for i := range inc.Actions {
		if inc.Actions[i].Kind == actionDisable && !inc.Actions[i].Reverted {
			n++
		}
	}
	return n
}

// Jars an open incident disabled, still on trial
func IncidentHeldFiles(dataPath string) []string {
	j := loadDoctor(dataPath)
	if j.Incident == nil {
		return nil
	}
	var files []string
	for i := range j.Incident.Actions {
		a := &j.Incident.Actions[i]
		if a.Kind == actionDisable && !a.Reverted {
			files = append(files, a.File)
		}
	}
	return files
}

func doctorPath(dataPath string) string {
	return filepath.Join(dataPath, runtimespec.StateDir, "doctor.json")
}

func loadDoctor(dataPath string) *doctorState {
	data, err := os.ReadFile(doctorPath(dataPath))
	if err != nil {
		return &doctorState{Version: 1}
	}
	var s doctorState
	if json.Unmarshal(data, &s) != nil {
		return &doctorState{Version: 1}
	}
	return &s
}

func saveDoctor(dataPath string, s *doctorState) error {
	if err := os.MkdirAll(filepath.Join(dataPath, runtimespec.StateDir), 0755); err != nil {
		return err
	}
	data, err := json.Marshal(s)
	if err != nil {
		return err
	}
	return os.WriteFile(doctorPath(dataPath), data, 0644)
}
