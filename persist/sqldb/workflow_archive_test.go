package sqldb

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/upper/db/v4"

	wfv1 "github.com/argoproj/argo-workflows/v4/pkg/apis/workflow/v1alpha1"
)

func Test_archivedWorkflowMetadata_argumentsUnmarshal(t *testing.T) {
	tests := []struct {
		name          string
		argumentsJSON string
		wantParams    int
		wantFirstName string
	}{
		{
			name:          "empty arguments",
			argumentsJSON: `{}`,
			wantParams:    0,
		},
		{
			name:          "with parameters",
			argumentsJSON: `{"parameters":[{"name":"message","value":"hello world"},{"name":"env","value":"production"}]}`,
			wantParams:    2,
			wantFirstName: "message",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			md := archivedWorkflowMetadata{Arguments: tt.argumentsJSON}
			arguments := wfv1.Arguments{}
			err := json.Unmarshal([]byte(md.Arguments), &arguments)
			require.NoError(t, err)
			assert.Len(t, arguments.Parameters, tt.wantParams)
			if tt.wantParams > 0 {
				assert.Equal(t, tt.wantFirstName, arguments.Parameters[0].Name)
			}
		})
	}
}

func Test_namespaceIn(t *testing.T) {
	// all namespaces: no condition
	assert.Equal(t, db.Cond{}, namespaceIn(nil))
	assert.Equal(t, db.Cond{}, namespaceIn([]string{}))
	// single namespace: plain equality
	assert.Equal(t, db.Cond{"namespace": "ns-1"}, namespaceIn([]string{"ns-1"}))
	// allowlist: IN clause
	assert.Equal(t, db.Cond{"namespace IN": []string{"ns-1", "ns-2"}}, namespaceIn([]string{"ns-1", "ns-2"}))
}
