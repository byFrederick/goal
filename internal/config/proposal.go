package config

import (
	"encoding/json"
	"errors"
	"fmt"
)

// ProposalCheck checks the files a Jonah proposal's edits give (ADR 0039 decision 6): the
// definition's blocking problems, and factory.yaml as a tree to compare before and after.
// jonah.DefinitionCheck is its port.
type ProposalCheck struct{}

// Problems are the messages of files' blocking problems, as Check reports them.
func (ProposalCheck) Problems(files map[string][]byte) []string {
	_, problems := Check(Files(files))

	var out []string

	for _, p := range problems {
		if p.Blocking() {
			out = append(out, problemError(p).Error())
		}
	}

	return out
}

// ParseFactory is factory.yaml's raw content as its JSON tree: maps, lists, strings, float64
// numbers, booleans and nil.
func (ProposalCheck) ParseFactory(raw []byte) (any, error) {
	doc, bad := parseDocument(raw)
	if bad != nil {
		return nil, errors.New(bad.Message)
	}

	var tree any
	if err := json.Unmarshal(doc.json, &tree); err != nil {
		return nil, fmt.Errorf("%s as JSON: %w", FileName, err)
	}

	return tree, nil
}
