package control

import (
	"encoding/json"
	"errors"
	"testing"

	app_errors "gpt-load/internal/platform/errors"
	"gpt-load/internal/state"
	"gpt-load/internal/storage/models"
)

func TestGroupPriorityPersistsPublishesAndResetsToZero(t *testing.T) {
	fixture := newServiceFixture(t)
	id := createGroupWithCredentials(t, fixture, "sk-priority")
	for _, priority := range []int{50, 0, state.MaxPriority} {
		result, err := fixture.service.UpdateGroupSettings(t.Context(), id, GroupSettingsUpdateRequest{
			Priority: optionalField[int]{Set: true, Value: priority},
		})
		if err != nil || result.Priority != priority {
			t.Fatalf("update priority %d = %#v, %v", priority, result, err)
		}
		var row models.Group
		if err := fixture.db.First(&row, id).Error; err != nil {
			t.Fatal(err)
		}
		snapshot := fixture.manager.Current()
		if row.Priority != priority || snapshot.Groups[id].Priority != priority || snapshot.GroupCatalog[id].Priority != priority {
			t.Fatal("priority did not propagate to database and runtime")
		}
		settings, err := fixture.service.GetGroupSettings(t.Context(), id)
		if err != nil || settings.Priority != priority {
			t.Fatalf("settings priority = %d, %v", settings.Priority, err)
		}
		workspace, err := fixture.service.ListModernGroups(t.Context())
		if err != nil || len(workspace.Items) != 1 || workspace.Items[0].Priority != priority {
			t.Fatalf("workspace priority = %#v, %v", workspace, err)
		}
	}
	before := fixture.manager.Current()
	for _, payload := range []string{`{"priority":null}`, `{"priority":-1}`, `{"priority":101}`, `{"priority":1000001}`} {
		var request GroupSettingsUpdateRequest
		if err := json.Unmarshal([]byte(payload), &request); err != nil {
			t.Fatal(err)
		}
		if _, err := fixture.service.UpdateGroupSettings(t.Context(), id, request); !errors.Is(err, app_errors.ErrValidation) {
			t.Fatalf("invalid priority %s error = %v", payload, err)
		}
		if fixture.manager.Current() != before {
			t.Fatal("invalid priority published a snapshot")
		}
	}
	_, err := fixture.service.UpdateGroupSettings(t.Context(), id, GroupSettingsUpdateRequest{
		WeightManual: optionalField[int]{Set: true, Value: 25},
	})
	if err != nil || fixture.manager.Current().Groups[id].Priority != state.MaxPriority {
		t.Fatalf("weight-only update lost priority: %v", err)
	}
}

func TestGroupCreatePriorityIncludesIdempotencyIdentity(t *testing.T) {
	fixture := newServiceFixture(t)
	var request GroupCreateRequest
	if err := json.Unmarshal([]byte(`{"channel_id":"openai","connection_type":"api_key","params":{},"models":[{"id":"gpt-4o","alias_enabled":false}],"credentials":"sk-priority","priority":42}`), &request); err != nil {
		t.Fatal(err)
	}
	const key = "218f47a2-9c35-4d6e-8b1a-1234567890ab"
	created, err := fixture.service.CreateGroupIdempotent(t.Context(), key, request)
	if err != nil {
		t.Fatal(err)
	}
	if fixture.manager.Current().Groups[created.GroupID].Priority != 42 {
		t.Fatal("create did not publish priority")
	}
	replayed, err := fixture.service.CreateGroupIdempotent(t.Context(), key, request)
	if err != nil || replayed.GroupID != created.GroupID {
		t.Fatalf("idempotency replay = %#v, %v", replayed, err)
	}
	request.Priority.Value = 43
	if _, err := fixture.service.CreateGroupIdempotent(t.Context(), key, request); err == nil {
		t.Fatal("different priority reused the same idempotency identity")
	}
}
