package pluginabi

import (
	"encoding/json"
	"testing"
)

func TestEnvelopeRoundTrip(t *testing.T) {
	payload := json.RawMessage(`{"name":"example"}`)
	env := Envelope{
		OK:     true,
		Result: payload,
	}

	raw, errMarshal := json.Marshal(env)
	if errMarshal != nil {
		t.Fatalf("marshal envelope: %v", errMarshal)
	}

	var decoded Envelope
	if errUnmarshal := json.Unmarshal(raw, &decoded); errUnmarshal != nil {
		t.Fatalf("unmarshal envelope: %v", errUnmarshal)
	}
	if !decoded.OK || string(decoded.Result) != string(payload) {
		t.Fatalf("decoded envelope = %#v, want ok payload", decoded)
	}
}

func TestMethodNamesAreStable(t *testing.T) {
	if SchemaVersion != 4 {
		t.Fatalf("SchemaVersion = %d, want 4", SchemaVersion)
	}
	if SchemaVersionWebSocketResponseObserver != 4 {
		t.Fatalf("SchemaVersionWebSocketResponseObserver = %d, want 4", SchemaVersionWebSocketResponseObserver)
	}
	if SchemaVersionStreamChunkOmitRequestBody != 3 {
		t.Fatalf("SchemaVersionStreamChunkOmitRequestBody = %d, want 3", SchemaVersionStreamChunkOmitRequestBody)
	}
	if SchemaVersionAIGC != 4 {
		t.Fatalf("SchemaVersionAIGC = %d, want 4", SchemaVersionAIGC)
	}
	if MethodContentGenerationStoreCreate != "content_generation.store.create" {
		t.Fatalf("MethodContentGenerationStoreCreate = %q", MethodContentGenerationStoreCreate)
	}
	if MethodContentGenerationMutate != "content_generation.mutate" {
		t.Fatalf("MethodContentGenerationMutate = %q", MethodContentGenerationMutate)
	}
	if MethodContentGenerationDriverSupports != "content_generation.driver.supports" {
		t.Fatalf("MethodContentGenerationDriverSupports = %q", MethodContentGenerationDriverSupports)
	}
	if MethodPluginRegister != "plugin.register" {
		t.Fatalf("MethodPluginRegister = %q", MethodPluginRegister)
	}
	if MethodPluginQuiesce != "plugin.quiesce" {
		t.Fatalf("MethodPluginQuiesce = %q", MethodPluginQuiesce)
	}
	if MethodRequestInterceptBefore != "request.intercept_before" {
		t.Fatalf("MethodRequestInterceptBefore = %q", MethodRequestInterceptBefore)
	}
	if MethodRequestInterceptAfter != "request.intercept_after" {
		t.Fatalf("MethodRequestInterceptAfter = %q", MethodRequestInterceptAfter)
	}
	if MethodRequestComplete != "request.complete" {
		t.Fatalf("MethodRequestComplete = %q", MethodRequestComplete)
	}
	if MethodResponseInterceptAfter != "response.intercept_after" {
		t.Fatalf("MethodResponseInterceptAfter = %q", MethodResponseInterceptAfter)
	}
	if MethodResponseInterceptStreamChunk != "response.intercept_stream_chunk" {
		t.Fatalf("MethodResponseInterceptStreamChunk = %q", MethodResponseInterceptStreamChunk)
	}
	if MethodWebSocketResponseEvent != "websocket.response_event" {
		t.Fatalf("MethodWebSocketResponseEvent = %q", MethodWebSocketResponseEvent)
	}
	if MethodHostHTTPDo != "host.http.do" {
		t.Fatalf("MethodHostHTTPDo = %q", MethodHostHTTPDo)
	}
	if MethodHostHTTPStreamRead != "host.http.stream_read" {
		t.Fatalf("MethodHostHTTPStreamRead = %q", MethodHostHTTPStreamRead)
	}
	if MethodHostModelExecute != "host.model.execute" {
		t.Fatalf("MethodHostModelExecute = %q", MethodHostModelExecute)
	}
	if MethodHostModelExecuteStream != "host.model.execute_stream" {
		t.Fatalf("MethodHostModelExecuteStream = %q", MethodHostModelExecuteStream)
	}
	if MethodHostModelStreamRead != "host.model.stream_read" {
		t.Fatalf("MethodHostModelStreamRead = %q", MethodHostModelStreamRead)
	}
	if MethodHostModelStreamClose != "host.model.stream_close" {
		t.Fatalf("MethodHostModelStreamClose = %q", MethodHostModelStreamClose)
	}
	if MethodHostAuthList != "host.auth.list" {
		t.Fatalf("MethodHostAuthList = %q", MethodHostAuthList)
	}
	if MethodHostAuthGet != "host.auth.get" {
		t.Fatalf("MethodHostAuthGet = %q", MethodHostAuthGet)
	}
	if MethodHostAuthGetRuntime != "host.auth.get_runtime" {
		t.Fatalf("MethodHostAuthGetRuntime = %q", MethodHostAuthGetRuntime)
	}
	if MethodHostAuthSave != "host.auth.save" {
		t.Fatalf("MethodHostAuthSave = %q", MethodHostAuthSave)
	}
	if MethodHostDatabaseQuery != "host.db.query" {
		t.Fatalf("MethodHostDatabaseQuery = %q", MethodHostDatabaseQuery)
	}
	if MethodHostDatabaseExec != "host.db.exec" {
		t.Fatalf("MethodHostDatabaseExec = %q", MethodHostDatabaseExec)
	}
	if MethodDatabaseProviderQuery != "database_provider.query" {
		t.Fatalf("MethodDatabaseProviderQuery = %q", MethodDatabaseProviderQuery)
	}
	if MethodDatabaseProviderExec != "database_provider.exec" {
		t.Fatalf("MethodDatabaseProviderExec = %q", MethodDatabaseProviderExec)
	}
	if MethodExecutorExecuteStream != "executor.execute_stream" {
		t.Fatalf("MethodExecutorExecuteStream = %q", MethodExecutorExecuteStream)
	}
}

func TestSchedulerPickMethodName(t *testing.T) {
	if MethodSchedulerPick != "scheduler.pick" {
		t.Fatalf("MethodSchedulerPick = %q", MethodSchedulerPick)
	}
	if MethodModelRoute != "model.route" {
		t.Fatalf("MethodModelRoute = %q", MethodModelRoute)
	}
}
