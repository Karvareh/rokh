package gate

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"rokh-home/native"
)

func nativeOutcomeRequest(namespace, intent, attempt, ending, saying string, witness map[string]any) map[string]any {
	// A later observation can resolve an unknown native effect. The old
	// observation stays in the append-only ledger; replaying either observation
	// returns its own receipt instead of conflicting with the other one.
	b, _ := json.Marshal([]string{namespace, intent, attempt, ending, saying})
	id := sha256.Sum256(b)
	return map[string]any{"op": "outcome", "address": namespace,
		"intent": intent, "outcome": ending, "saying": saying,
		"attempt": "native-outcome:" + hex.EncodeToString(id[:]), "witness": witness}
}

func nativeCompletion(answer map[string]any, result native.PublishResult, outcome map[string]any) map[string]any {
	id, _ := outcome["id"].(string)
	_, idErr := hex.DecodeString(id)
	if outcome["ok"] != true || outcome["record"] != "recorded" || len(id) != 64 || idErr != nil {
		// The native effect and our durable receipt are distinct. Never erase an
		// observed native success, or call the whole pipeline complete when its
		// outcome is missing. The same publication attempt can safely be retried.
		answer["ok"] = false
		answer["record"] = "unknown"
		answer["stage"] = "outcome"
		answer["code"] = "outcome_unknown"
		answer["error"] = "the native result was observed, but its ledger outcome is not confirmed; inspect and retry the same attempt"
		return answer
	}
	if result.Record == "recorded" {
		return okWith(answer)
	}
	answer["ok"] = false
	answer["code"] = "native_" + result.Record
	answer["error"] = result.Error
	return answer
}
