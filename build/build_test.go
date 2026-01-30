package build

import (
	"os"
	"testing"

	"github.com/jfrog/build-info-go/entities"
	"github.com/stretchr/testify/assert"
)

func TestCollectEnv(t *testing.T) {
	tests := []struct {
		description string
		include     []string
		exclude     []string
		expected    entities.Env
		expectError bool
	}{
		{
			description: "just include",
			include:     []string{"BI_TEST_COLLECT_*", "BI_TEST_ALSO_cOLLeCt"},
			exclude:     nil,
			expected: entities.Env{
				"buildInfo.env.BI_TEST_COLLECT_1":    "val",
				"buildInfo.env.BI_TEST_COLLECT_2":    "val",
				"buildInfo.env.BI_TEST_ALSO_COLLECT": "val",
			},
			expectError: false,
		},
		{
			description: "include and exclude",
			include:     []string{"BI_TEST_*"},
			exclude:     []string{"BI_TEST_DoNt_*", "*ALSO*"},
			expected: entities.Env{
				"buildInfo.env.BI_TEST_COLLECT_1": "val",
				"buildInfo.env.BI_TEST_COLLECT_2": "val",
			},
			expectError: false,
		},
	}

	env := entities.Env{
		"BI_TEST_COLLECT_1":         "val",
		"BI_TEST_COLLECT_2":         "val",
		"BI_TEST_ALSO_COLLECT":      "val",
		"BI_TEST_DONT_COLLECT":      "val",
		"BI_TEST_ALSO_DONT_COLLECT": "val",
	}

	// Set environment variables
	defer func() {
		for key := range env {
			assert.NoError(t, os.Unsetenv(key))
		}
	}()
	for key, value := range env {
		assert.NoError(t, os.Setenv(key, value))
	}

	service := NewBuildInfoService()
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			build, err := service.GetOrCreateBuild("bi-test", "1")
			assert.NoError(t, err)
			assert.NoError(t, build.CollectEnv())
			buildInfo, err := build.ToBuildInfo()
			assert.NoError(t, err)
			err = buildInfo.IncludeEnv(tc.include...)
			if tc.expectError {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			err = buildInfo.ExcludeEnv(tc.exclude...)
			assert.NoError(t, err)
			assert.Equal(t, tc.expected, buildInfo.Properties)
			assert.Empty(t, buildInfo.Modules)
			err = build.Clean()
			assert.NoError(t, err)
		})
	}
}

func TestSortBuildInfosByTimestamp(t *testing.T) {
	tests := []struct {
		name          string
		buildInfos    []*entities.BuildInfo
		expectedOrder []string // Expected order of Started timestamps
	}{
		{
			name: "already sorted",
			buildInfos: []*entities.BuildInfo{
				{Name: "build1", Started: "2026-01-30T10:00:00.000+0000"},
				{Name: "build2", Started: "2026-01-30T11:00:00.000+0000"},
				{Name: "build3", Started: "2026-01-30T12:00:00.000+0000"},
			},
			expectedOrder: []string{
				"2026-01-30T10:00:00.000+0000",
				"2026-01-30T11:00:00.000+0000",
				"2026-01-30T12:00:00.000+0000",
			},
		},
		{
			name: "reverse order",
			buildInfos: []*entities.BuildInfo{
				{Name: "build3", Started: "2026-01-30T12:00:00.000+0000"},
				{Name: "build2", Started: "2026-01-30T11:00:00.000+0000"},
				{Name: "build1", Started: "2026-01-30T10:00:00.000+0000"},
			},
			expectedOrder: []string{
				"2026-01-30T10:00:00.000+0000",
				"2026-01-30T11:00:00.000+0000",
				"2026-01-30T12:00:00.000+0000",
			},
		},
		{
			name: "mixed order - Maven install then deploy scenario",
			buildInfos: []*entities.BuildInfo{
				{Name: "deploy", Started: "2026-01-30T10:05:30.000+0000"},  // deploy (later)
				{Name: "install", Started: "2026-01-30T10:05:00.000+0000"}, // install (earlier)
			},
			expectedOrder: []string{
				"2026-01-30T10:05:00.000+0000", // install first
				"2026-01-30T10:05:30.000+0000", // deploy second
			},
		},
		{
			name:          "empty list",
			buildInfos:    []*entities.BuildInfo{},
			expectedOrder: []string{},
		},
		{
			name: "single item",
			buildInfos: []*entities.BuildInfo{
				{Name: "build1", Started: "2026-01-30T10:00:00.000+0000"},
			},
			expectedOrder: []string{"2026-01-30T10:00:00.000+0000"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sortBuildInfosByTimestamp(tc.buildInfos)
			for i, expected := range tc.expectedOrder {
				assert.Equal(t, expected, tc.buildInfos[i].Started,
					"Position %d: expected %s, got %s", i, expected, tc.buildInfos[i].Started)
			}
		})
	}
}
