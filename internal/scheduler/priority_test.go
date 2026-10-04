package scheduler

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"gpt-load/internal/channel"
	"gpt-load/internal/execution"
	"gpt-load/internal/protocol"
	"gpt-load/internal/state"
)

func prioritySnapshot(t *testing.T, priorities, weights []int) *state.ConfigSnapshot {
	t.Helper()
	input := state.CompileInput{ChannelRegistry: channel.NewRegistry()}
	for i, priority := range priorities {
		input.Groups = append(input.Groups, state.GroupConfig{
			ID: uint(i + 1), ChannelID: channel.OpenAI, ConnectionType: "api_key",
			Params: json.RawMessage(`{}`), Models: []state.ModelConfig{{ID: "gpt-4o"}},
			Enabled: true, Priority: priority, WeightManual: &weights[i],
		})
	}
	snapshot, err := state.Compile(input)
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func TestPriorityExhaustsHigherTiersBeforeFallback(t *testing.T) {
	snapshot := prioritySnapshot(t, []int{0, 10, 20}, []int{100, 100, 1})
	source := fakeCredentialSource{keys: []state.CredentialMeta{
		{ID: 11, GroupID: 1}, {ID: 21, GroupID: 2}, {ID: 31, GroupID: 3}, {ID: 32, GroupID: 3},
	}}
	iterator := New(snapshot, source, fairnessQuery(11))
	for _, want := range []uint{3, 3, 2, 1} {
		selection, err := iterator.Next()
		if err != nil || selection.GroupID != want {
			t.Fatalf("Next() = (%#v, %v), want priority group %d", selection, err, want)
		}
	}
	if _, err := iterator.Next(); !errors.Is(err, ErrExhausted) {
		t.Fatalf("exhausted Next() = %v", err)
	}
	// Failure exclusions are per request; a new request starts with the top tier.
	selection, err := New(snapshot, source, fairnessQuery(0)).Next()
	if err != nil || selection.GroupID != 3 {
		t.Fatalf("new request = (%#v, %v), want group 3", selection, err)
	}
}

func TestPriorityUsesWeightsOnlyWithinHighestAvailableTier(t *testing.T) {
	snapshot := prioritySnapshot(t, []int{10, 10, 0}, []int{25, 75, 100})
	source := fakeCredentialSource{keys: []state.CredentialMeta{
		{ID: 11, GroupID: 1}, {ID: 21, GroupID: 2}, {ID: 31, GroupID: 3},
	}}
	source.progress = source.SchedulingState()
	counts := make(map[uint]int)
	for range 400 {
		selection, err := New(snapshot, source, fairnessQuery(0)).Next()
		if err != nil {
			t.Fatal(err)
		}
		counts[selection.GroupID]++
	}
	if counts[1] != 100 || counts[2] != 300 || counts[3] != 0 {
		t.Fatalf("distribution = %v, want 100:300:0", counts)
	}
}

func TestPriorityPrecedesNativeRoutePreference(t *testing.T) {
	snapshot := channelSchedulerSnapshot(t)
	group := snapshot.Groups[1]
	group.Priority = 10
	snapshot.Groups[1] = group
	iterator := New(snapshot, fakeCredentialSource{keys: []state.CredentialMeta{
		{ID: 11, GroupID: 1}, {ID: 21, GroupID: 2},
	}}, Query{
		ClientProtocol: protocol.OpenAICompletions, Operation: execution.OperationChatCompletion,
		ExternalModel: modelPointer("public"), PreferredCredentialID: 21,
	})
	for _, want := range []uint{1, 2} {
		selection, err := iterator.Next()
		if err != nil || selection.GroupID != want {
			t.Fatalf("Next() = (%#v, %v), want group %d", selection, err, want)
		}
	}
}

func TestPrioritySkipsUnavailableOrExcludedHigherGroups(t *testing.T) {
	for _, scenario := range []string{"no credentials", "disabled", "cooldown", "blacklisted", "zero weight", "access filter", "credential filter", "skip group", "model cooldown"} {
		t.Run(scenario, func(t *testing.T) {
			snapshot := prioritySnapshot(t, []int{0, 10}, []int{100, 1})
			query := fairnessQuery(0)
			now := time.Now()
			entries := []state.CredentialEntry{
				{ID: 11, GroupID: 1, Status: state.CredentialStatusActive, Version: 1, IdentityGeneration: 1, Fingerprint: "low", EncryptedValue: "cipher"},
				{ID: 21, GroupID: 2, Status: state.CredentialStatusActive, Version: 1, IdentityGeneration: 1, Fingerprint: "high", EncryptedValue: "cipher"},
			}
			switch scenario {
			case "no credentials":
				entries = entries[:1]
			case "disabled":
				entries[1].Status = state.CredentialStatusDisabled
			case "cooldown":
				entries[1].CooldownUntil = now.Add(time.Hour)
			case "blacklisted":
				entries[1].Blacklisted = true
			case "zero weight":
				group := snapshot.Groups[2]
				group.WeightManual = new(int)
				snapshot.Groups[2] = group
			case "access filter":
				query.AccessKey.Filters.Groups = map[uint]struct{}{1: {}}
			case "credential filter":
				query.AllowedCredentialIDs = map[uint]struct{}{11: {}}
			}
			registry := state.NewCredentialRegistry()
			if err := registry.ReplaceCredentials(entries); err != nil {
				t.Fatal(err)
			}
			if scenario == "model cooldown" {
				ref, _ := registry.CredentialRef(21)
				registry.SetModelCooldown(ref, "gpt-4o", now.Add(time.Hour), now)
			}
			iterator := New(snapshot, registry, query)
			if scenario == "skip group" {
				iterator.SkipGroup(2)
			}
			selection, err := iterator.Next()
			if err != nil || selection.GroupID != 1 {
				t.Fatalf("Next() = (%#v, %v), want available fallback", selection, err)
			}
		})
	}
}
