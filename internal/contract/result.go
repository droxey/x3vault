package contract

type Diagnostic struct {
	Level   string `json:"level"` // "error", "warning", "info"
	Message string `json:"message"`
	Path    string `json:"path,omitempty"`
}

type Summary struct {
	Notes    int `json:"notes"`
	Assets   int `json:"assets"`
	Warnings int `json:"warnings"`
	Errors   int `json:"errors"`
}

type Result struct {
	Schema      int          `json:"schema"`
	Command     string       `json:"command"`
	OK          bool         `json:"ok"`
	Generation  string       `json:"generation,omitempty"`
	Summary     Summary      `json:"summary"`
	Diagnostics []Diagnostic `json:"diagnostics"`
}

func NewResult(cmd string) *Result {
	return &Result{
		Schema:      1,
		Command:     cmd,
		OK:          true,
		Diagnostics: []Diagnostic{},
	}
}

func (r *Result) AddError(msg, path string) {
	r.OK = false
	r.Summary.Errors++
	r.Diagnostics = append(r.Diagnostics, Diagnostic{Level: "error", Message: msg, Path: path})
}

func (r *Result) AddWarning(msg, path string) {
	r.Summary.Warnings++
	r.Diagnostics = append(r.Diagnostics, Diagnostic{Level: "warning", Message: msg, Path: path})
}
