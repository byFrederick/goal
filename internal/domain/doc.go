// Package domain holds Goal's plain types and pure rules: work items, the line of stations
// they flow through, leases, agent runs, events and gate verdicts.
//
// It imports only the standard library. internal/config builds a Line from a factory file;
// the engine, ports and adapters all speak these types.
package domain
