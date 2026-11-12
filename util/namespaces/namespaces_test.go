package namespaces

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParse(t *testing.T) {
	assert.Equal(t, []string(nil), Parse(""))
	assert.Empty(t, Parse(",,,"))
	assert.Equal(t, []string{"ns-1"}, Parse("ns-1"))
	assert.Equal(t, []string{"ns-1", "ns-2", "ns-3"}, Parse("ns-1,ns-2,ns-3"))
	assert.Equal(t, []string{"ns-1", "ns-2"}, Parse(" ns-1 , ns-2 "))
	assert.Equal(t, []string{"ns-1", "ns-2", "ns-3"}, Parse("ns-1,,ns-2,ns-3"))
}

func TestFormat(t *testing.T) {
	assert.Empty(t, Format(nil))
	assert.Empty(t, Format([]string{}))
	assert.Equal(t, "ns-1", Format([]string{"ns-1"}))
	assert.Equal(t, "ns-1,ns-2", Format([]string{"ns-1", "ns-2"}))
}

func TestIsMultiple(t *testing.T) {
	assert.False(t, IsMultiple(""))
	assert.False(t, IsMultiple("ns-1"))
	assert.True(t, IsMultiple("ns-1,ns-2"))
	assert.True(t, IsMultiple("ns-1,ns-2,ns-3"))
}

func TestParseOrAll(t *testing.T) {
	assert.Equal(t, []string{""}, ParseOrAll(""))
	assert.Equal(t, []string{"ns-1"}, ParseOrAll("ns-1"))
	assert.Equal(t, []string{"ns-1", "ns-2"}, ParseOrAll("ns-1,ns-2"))
}
