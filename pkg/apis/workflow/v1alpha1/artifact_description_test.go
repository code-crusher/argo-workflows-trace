package v1alpha1

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"
)

func TestArtifactDescriptionJSONSerialization(t *testing.T) {
	in := &Artifact{
		Name:        "input-data",
		Description: AnyStringPtr("Upload a UTF-8 encoded CSV file with a header row."),
	}
	// Marshal to JSON via the CRD-aware YAML JSON round trip (sigs.k8s.io/yaml
	// converts YAML to JSON), then unmarshal back and verify the description
	// survives.
	data, err := yaml.Marshal(in)
	require.NoError(t, err)
	out := &Artifact{}
	require.NoError(t, yaml.Unmarshal(data, out))
	assert.Equal(t, "input-data", out.Name)
	require.NotNil(t, out.Description)
	assert.Equal(t, AnyString("Upload a UTF-8 encoded CSV file with a header row."), *out.Description)
}

func TestArtifactDescriptionOptional(t *testing.T) {
	in := &Artifact{Name: "input-data"}
	data, err := yaml.Marshal(in)
	require.NoError(t, err)
	// The description must be omitted entirely when not set.
	assert.NotContains(t, string(data), "description")
	out := &Artifact{}
	require.NoError(t, yaml.Unmarshal(data, out))
	assert.Nil(t, out.Description)
}