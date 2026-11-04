package jonah_test

import (
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
)

// The factory and the version the tests' Jonah works on.
const (
	factory = domain.FactoryID("00000000-0000-4000-8000-000000000001")
	version = domain.DefinitionVersionID("00000000-0000-4000-a000-000000000001")
)

var t0 = time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

// The reviewer agent and the build station of the tests' lines.
const (
	agentReviewer = "reviewer"
	stationBuild  = "build"
)
