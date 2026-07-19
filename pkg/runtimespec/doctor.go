package runtimespec

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"time"

	v1 "github.com/nickheyer/discopanel/pkg/proto/discopanel/v1"
)

// Shared journal contract between the panel and the doctor module

const DoctorFileName = "doctor.json"

// Action kinds a doctor incident may record
const (
	ActionDisable     = "disable"
	ActionEnable      = "enable"
	ActionInstall     = "install"
	ActionDisablePack = "disable_pack"
)

// Evidence grades ordered strongest first
const (
	EvidenceVerdict  = "verdict"
	EvidenceSolver   = "solver"
	EvidenceRegistry = "registry"
	EvidenceFrame    = "frame"
)

// Durable record of the doctor's work on one server
type DoctorState struct {
	Version  int             `json:"version"`
	Incident *DoctorIncident `json:"incident,omitempty"`
	Resolved *DoctorIncident `json:"resolved,omitempty"`

	// Files the doctor never wants provisioned again
	Excludes []string `json:"excludes,omitempty"`

	// Newest exit the doctor already responded to
	LastHandledMs int64 `json:"last_handled_ms,omitempty"`
}

// One repair campaign, opened on crash and closed on outcome
type DoctorIncident struct {
	OpenedAt time.Time      `json:"opened_at"`
	ClosedAt time.Time      `json:"closed_at,omitzero"`
	Passes   int            `json:"passes"`
	Budget   int            `json:"budget"`
	Actions  []DoctorAction `json:"actions"`
	Tried    []string       `json:"tried,omitempty"`
	Outcome  string         `json:"outcome,omitempty"`
	Summary  string         `json:"summary,omitempty"`
	Cause    string         `json:"cause,omitempty"` // Plain language crash classification
}

// One reversible repair step
type DoctorAction struct {
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
func (a *DoctorAction) Key() string {
	if a.Kind == ActionInstall {
		return a.Kind + ":" + a.ModID
	}
	return a.Kind + ":" + a.File
}

func (inc *DoctorIncident) HasTried(key string) bool {
	return slices.Contains(inc.Tried, key)
}

func (inc *DoctorIncident) MarkTried(key string) {
	if !inc.HasTried(key) {
		inc.Tried = append(inc.Tried, key)
	}
}

// Counts live disables, the budget consumers
func (inc *DoctorIncident) DisabledCount() int {
	n := 0
	for i := range inc.Actions {
		if inc.Actions[i].Kind == ActionDisable && !inc.Actions[i].Reverted {
			n++
		}
	}
	return n
}

// Time of the incident's newest activity
func (inc *DoctorIncident) LastActivity() time.Time {
	last := inc.OpenedAt
	for i := range inc.Actions {
		if inc.Actions[i].AppliedAt.After(last) {
			last = inc.Actions[i].AppliedAt
		}
	}
	return last
}

// Jars an open incident disabled, still on trial
func IncidentHeldFiles(dataPath string) []string {
	j := LoadDoctor(dataPath)
	if j.Incident == nil {
		return nil
	}
	var files []string
	for i := range j.Incident.Actions {
		a := &j.Incident.Actions[i]
		if a.Kind == ActionDisable && !a.Reverted {
			files = append(files, a.File)
		}
	}
	return files
}

// Files the doctor permanently excluded from provisioning
func DoctorExcludes(dataPath string) []string {
	return LoadDoctor(dataPath).Excludes
}

func DoctorPath(dataPath string) string {
	return filepath.Join(dataPath, StateDir, DoctorFileName)
}

func LoadDoctor(dataPath string) *DoctorState {
	data, err := os.ReadFile(DoctorPath(dataPath))
	if err != nil {
		return &DoctorState{Version: 1}
	}
	var s DoctorState
	if json.Unmarshal(data, &s) != nil {
		return &DoctorState{Version: 1}
	}
	return &s
}

func SaveDoctor(dataPath string, s *DoctorState) error {
	if err := os.MkdirAll(filepath.Join(dataPath, StateDir), 0755); err != nil {
		return err
	}
	data, err := json.Marshal(s)
	if err != nil {
		return err
	}
	return os.WriteFile(DoctorPath(dataPath), data, 0644)
}

// Findings file holds PerformanceFinding messages as protojson

const FindingsFileName = "findings.json"

func findingsPath(dataPath string) string {
	return filepath.Join(dataPath, StateDir, FindingsFileName)
}

// Reads doctor-published findings, empty when absent
func ReadFindings(dataPath string) []*v1.PerformanceFinding {
	return readProtoList(findingsPath(dataPath), func() *v1.PerformanceFinding { return &v1.PerformanceFinding{} })
}

// Publishes the doctor's current findings for one server
func WriteFindings(dataPath string, findings []*v1.PerformanceFinding) error {
	if err := os.MkdirAll(filepath.Join(dataPath, StateDir), 0755); err != nil {
		return err
	}
	data, err := marshalProtoList(findings)
	if err != nil {
		return err
	}
	return os.WriteFile(findingsPath(dataPath), data, 0644)
}
