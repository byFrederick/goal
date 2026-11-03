package cli

import (
	"errors"
	"strings"
	"testing"
)

const cmdMemory = "memory"

// goal memory add appends facts to the draft's memory.yaml, list numbers them and rm removes one;
// nothing is published.
func TestMemory_AddListRemove(t *testing.T) {
	t.Parallel()

	g := newDefGoald(t)
	g.mustGoal(t, "factory", "push", newCopy(t, nil), "--publish")

	expectOutput(t, "list of none", g.mustGoal(t, cmdMemory, "list", "acme"), []string{"acme has no factory memory"})

	expectOutput(t, "add", g.mustGoal(t, cmdMemory, "add", "acme", "staging-db disk alerts below 85% are noise", "--from", "acme#225,acme#231"),
		[]string{"added fact 1 to acme's factory memory in its draft", "goal factory publish acme"})
	g.mustGoal(t, cmdMemory, "add", "acme", "Deploys go out on Tuesdays")

	expectOutput(t, "list", g.mustGoal(t, cmdMemory, "list", "acme"),
		[]string{"1. staging-db disk alerts below 85% are noise (from acme#225, acme#231)", "2. Deploys go out on Tuesdays"})

	expectOutput(t, "rm", g.mustGoal(t, cmdMemory, "rm", "acme", "1"), []string{"removed fact 1"})

	got := g.draftFile(t, "draft", "memory.yaml")
	expectOutput(t, "memory.yaml", got, []string{"- text: Deploys go out on Tuesdays", "added: "}, "staging-db")
	expectOutput(t, "the current version", g.draftFile(t, "current", "memory.yaml"), nil, "Tuesdays")

	if _, err := g.goal(t, cmdMemory, "rm", "acme", "5"); err == nil || !strings.Contains(err.Error(), "has 1 facts") {
		t.Errorf("rm of no such fact = %v; want it refused", err)
	}

	for _, args := range [][]string{{"list"}, {"add", "acme"}, {"add", "acme", " "}, {"rm", "acme", "x"}, {"forget"}} {
		if _, err := g.goal(t, append([]string{cmdMemory}, args...)...); !errors.Is(err, errUsage) {
			t.Errorf("goal memory %s = %v; want the usage", strings.Join(args, " "), err)
		}
	}
}

// goal memory add --publish publishes the draft with the fact.
func TestMemory_AddPublish(t *testing.T) {
	t.Parallel()

	g := newDefGoald(t)
	g.mustGoal(t, "factory", "push", newCopy(t, nil), "--publish")

	expectOutput(t, "add --publish", g.mustGoal(t, cmdMemory, "add", "acme", "Deploys go out on Tuesdays", "--publish"),
		[]string{"added fact 1", "published acme v2"})
	expectOutput(t, "the current version", g.draftFile(t, "current", "memory.yaml"), []string{"Deploys go out on Tuesdays"})
}
