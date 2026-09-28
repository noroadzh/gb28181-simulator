package scenario

import (
	"strings"
	"testing"
)

const testFilename = "examples/register-keepalive-catalog.yaml"

func validYAML(extra string) string {
	return "name: register-keepalive-catalog\ndescription: drill\nsteps:\n" + extra + "\n"
}

func TestLoadScenarioBytes_Valid(t *testing.T) {
	data := []byte(validYAML(`
- type: create-node
  params:
    id: dev01
    profile: device
- type: start-node
  params:
    id: dev01
`))
	s, err := LoadScenarioBytes(testFilename, data)
	if err != nil {
		t.Fatalf("LoadScenarioBytes() = error %v", err)
	}
	if s.Name != "register-keepalive-catalog" {
		t.Fatalf("Name = %q, want register-keepalive-catalog", s.Name)
	}
	if len(s.Steps) != 2 {
		t.Fatalf("len(Steps) = %d, want 2", len(s.Steps))
	}
	if s.Steps[0].Type != "create-node" {
		t.Fatalf("step0.Type = %q, want create-node", s.Steps[0].Type)
	}
}

func TestLoadScenarioBytes_Errors(t *testing.T) {
	cases := []struct {
		name        string
		payload     string
		expected    string
		mustContain []string
	}{
		{
			name:        "missing name",
			payload:     "description: missing-name\nsteps:\n- type: wait\n",
			expected:    "field is required",
			mustContain: []string{"name"},
		},
		{
			name:        "missing steps",
			payload:     "name: no-steps\ndescription: missing steps\n",
			expected:    "at least one step is required",
			mustContain: []string{"steps"},
		},
		{
			name:        "step missing type",
			payload:     "name: bad-step\nsteps:\n- params:\n    id: 1\n",
			expected:    "steps[0].type: field is required",
			mustContain: []string{"steps[0].type"},
		},
		{
			name:        "unknown step type",
			payload:     "name: bad-type\nsteps:\n- type: teleport\n",
			expected:    "unknown step type \"teleport\"",
			mustContain: []string{"steps[0].type", "teleport"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := LoadScenarioBytes(testFilename, []byte(tc.payload))
			if err == nil {
				t.Fatalf("LoadScenarioBytes() = nil, want error containing %q", tc.expected)
			}
			msg := err.Error()
			if !strings.Contains(msg, tc.expected) {
				t.Fatalf("error = %q, want to contain %q", msg, tc.expected)
			}
			for _, sub := range tc.mustContain {
				if !strings.Contains(msg, sub) {
					t.Fatalf("error = %q, want to contain %q", msg, sub)
				}
			}
		})
	}
}
