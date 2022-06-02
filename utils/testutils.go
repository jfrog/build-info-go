package utils

import (
	"github.com/stretchr/testify/assert"
	"os"
	"testing"
)

func RemoveAndAssert(t *testing.T, path string) {
	assert.NoError(t, os.Remove(path), "Couldn't remove: "+path)
}
