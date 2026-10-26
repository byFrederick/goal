package store

import "os"

var shared = os.Getenv("GOAL_SHARED")
