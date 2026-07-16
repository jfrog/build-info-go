package build

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jfrog/build-info-go/entities"
	"github.com/jfrog/build-info-go/utils"
	"github.com/jfrog/build-info-go/utils/cienv"
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

// TestBuildInfoDurationMillis verifies that the generated build-info carries the elapsed
// time between the build's start and the moment the build-info is generated (publish time).
func TestBuildInfoDurationMillis(t *testing.T) {
	tmpDir, err := utils.CreateTempDir()
	assert.NoError(t, err)
	defer func() { assert.NoError(t, utils.RemoveTempDir(tmpDir)) }()

	const buildName, buildNumber = "duration-test", "1"
	service := NewBuildInfoService()
	service.SetTempDirPath(tmpDir)

	build, err := service.GetOrCreateBuild(buildName, buildNumber)
	assert.NoError(t, err)
	defer func() { assert.NoError(t, build.Clean()) }()

	// Duration is only recorded in CI environments.
	t.Setenv("CI", "true")

	// Simulate a build that started one hour ago by rewriting the persisted start time.
	startedAt := time.Now().Add(-time.Hour)
	writeBuildGeneralDetails(t, tmpDir, buildName, buildNumber, startedAt)

	buildInfo, err := build.ToBuildInfo()
	assert.NoError(t, err)

	// Started must reflect the recorded start time.
	parsedStart, err := time.Parse(entities.TimeFormat, buildInfo.Started)
	assert.NoError(t, err)
	assert.WithinDuration(t, startedAt, parsedStart, time.Second)

	// Duration must be ~1 hour, and crucially not 0.
	assert.Greater(t, buildInfo.DurationMillis, int64(0))
	assert.InDelta(t, time.Hour.Milliseconds(), buildInfo.DurationMillis, float64((30 * time.Second).Milliseconds()))
}

// TestBuildInfoDurationMillisSkippedOutsideCI verifies the duration is not recorded when not in CI.
func TestBuildInfoDurationMillisSkippedOutsideCI(t *testing.T) {
	tmpDir, err := utils.CreateTempDir()
	assert.NoError(t, err)
	defer func() { assert.NoError(t, utils.RemoveTempDir(tmpDir)) }()

	const buildName, buildNumber = "duration-test-noci", "1"
	service := NewBuildInfoService()
	service.SetTempDirPath(tmpDir)

	build, err := service.GetOrCreateBuild(buildName, buildNumber)
	assert.NoError(t, err)
	defer func() { assert.NoError(t, build.Clean()) }()

	// Ensure no CI markers are present.
	t.Setenv("CI", "")
	for _, envVar := range cienv.CIEnvIndicators {
		t.Setenv(envVar, "")
	}

	writeBuildGeneralDetails(t, tmpDir, buildName, buildNumber, time.Now().Add(-time.Hour))

	buildInfo, err := build.ToBuildInfo()
	assert.NoError(t, err)

	// Started is still recorded, but duration must remain 0 outside CI.
	assert.NotEmpty(t, buildInfo.Started)
	assert.Equal(t, int64(0), buildInfo.DurationMillis)
}

// writeBuildGeneralDetails overwrites the persisted build "details" file with the given start time.
func writeBuildGeneralDetails(t *testing.T, tmpDir, buildName, buildNumber string, startedAt time.Time) {
	partialsBuildDir, err := utils.GetPartialsBuildDir(buildName, buildNumber, "", tmpDir)
	assert.NoError(t, err)
	content, err := json.Marshal(&entities.General{Timestamp: startedAt})
	assert.NoError(t, err)
	assert.NoError(t, os.WriteFile(filepath.Join(partialsBuildDir, BuildInfoDetails), content, 0600))
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
