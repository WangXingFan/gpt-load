package gateway

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"gpt-load/internal/channel"
	"gpt-load/internal/execution"
	"gpt-load/internal/platform/config"
	"gpt-load/internal/state"
)

func TestHandlerPriorityFallsBackOnlyAfterRetryableFailure(t *testing.T) {
	for _, test := range []struct {
		name       string
		statuses   []int
		retries    int
		wantGroups []uint
		wantStatus int
	}{
		{"success stays in highest tier", []int{200}, 2, []uint{3}, 200},
		{"retryable failures descend through tiers", []int{401, 401, 200}, 2, []uint{3, 2, 1}, 200},
		{"retry budget still limits fallback", []int{401}, 0, []uint{3}, 401},
		{"invalid request is not retried", []int{400}, 2, []uint{3}, 400},
	} {
		t.Run(test.name, func(t *testing.T) {
			forwarder := &scriptedForwarder{}
			for _, status := range test.statuses {
				body := []byte(`{"id":"ok","model":"gpt-4o","choices":[{"message":{"role":"assistant","content":"ok"}}]}`)
				if status != 200 {
					body = []byte(`{"error":"invalid_api_key"}`)
					if status == 400 {
						body = []byte(`{"error":{"type":"invalid_request_error","code":"context_length_exceeded","message":"context too long"}}`)
					}
				}
				forwarder.results = append(forwarder.results, UpstreamResult{
					StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}},
					Body: body, ClassificationBody: body, RequestWritten: true,
				})
				if status == http.StatusBadRequest {
					forwarder.results[len(forwarder.results)-1].ExecutionError = &execution.ErrorEvidence{
						Kind: execution.ErrorKindHTTP, Code: "context_length_exceeded", OriginHint: execution.ErrorOriginUpstream,
					}
				}
			}
			handler, manager, registry := newHandlerForTest(t, forwarder, "sk-low", "sk-middle", "sk-high")
			entries, err := registry.SnapshotGroupCredentialEntriesExact(1, []uint{1, 2, 3})
			if err != nil {
				t.Fatal(err)
			}
			input := state.CompileInput{
				SystemSettings: config.Settings{state.SettingRetryCount: test.retries}, ChannelRegistry: channel.NewRegistry(),
				AccessKeys: []state.AccessKeyConfig{{ID: 1, Name: "client", KeyHash: handler.encryption.Hash("gl-client"), Status: state.AccessKeyStatusActive}},
			}
			for i := range entries {
				id := uint(i + 1)
				entries[i].GroupID = id
				weight := 100 - i*40
				input.Groups = append(input.Groups, state.GroupConfig{
					ID: id, Name: fmt.Sprintf("priority-%d", id), Priority: i * 10, WeightManual: &weight,
					ChannelID: channel.OpenAI, ConnectionType: "api_key", Params: json.RawMessage(`{}`),
					Models: []state.ModelConfig{{ID: "gpt-4o"}}, Enabled: true,
				})
				input.Credentials = append(input.Credentials, state.CredentialConfig{
					ID: id, GroupID: id, Status: entries[i].Status, Version: entries[i].Version,
					IdentityGeneration: entries[i].IdentityGeneration, Fingerprint: entries[i].Fingerprint,
				})
			}
			if err := registry.ReplaceCredentials(entries); err != nil {
				t.Fatal(err)
			}
			if _, err := manager.Publish(input); err != nil {
				t.Fatal(err)
			}
			engine := gin.New()
			bindGatewayRoutesForTest(t, engine, handler)
			request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-4o"}`))
			request.Header.Set("Authorization", "Bearer gl-client")
			response := httptest.NewRecorder()
			engine.ServeHTTP(response, request)
			if response.Code != test.wantStatus || len(forwarder.inputs) != len(test.wantGroups) {
				t.Fatalf("status/attempts = %d/%d, body=%s", response.Code, len(forwarder.inputs), response.Body.String())
			}
			for i, want := range test.wantGroups {
				if forwarder.inputs[i].Group.ID != want {
					t.Fatalf("attempt %d group = %d, want %d", i, forwarder.inputs[i].Group.ID, want)
				}
			}
		})
	}
}
