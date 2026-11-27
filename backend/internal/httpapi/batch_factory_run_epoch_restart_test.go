package httpapi

import (
	"encoding/json"
	"testing"

	"qiantie/backend/internal/batchfactoryv11"
)

func TestAutomationLaunchAfterRestartDoesNotReusePersistedClaimEpoch(t *testing.T) {
	for _, tc := range []struct {
		name    string
		current int64
		want    int64
	}{
		{name: "new process", current: 0, want: 12},
		{name: "in memory behind persisted claims", current: 3, want: 12},
		{name: "existing process ahead", current: 20, want: 21},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, batch, state := priorityFixture(t)
			job := state.state.Jobs["job-active"]
			entries := job["books"].(map[string]any)
			entries[batch.Books[0].ID].(map[string]any)[automationBookClaimEpochField] = 1
			entries[batch.Books[1].ID].(map[string]any)[automationBookClaimEpochField] = 11
			// Use the JSON shape loaded from MySQL after a real process restart.
			encoded, err := json.Marshal(job)
			if err != nil {
				t.Fatal(err)
			}
			var restored map[string]any
			if err := json.Unmarshal(encoded, &restored); err != nil {
				t.Fatal(err)
			}
			c.runEpochs = map[string]int64{"job-active": tc.current}
			// Block the background runner until its launch identity has been read
			// and cancelled. No book generation or external request is needed.
			c.mu.Lock()
			c.launchRun("alice", batch.ID, "job-active", restored, batchfactoryv11.Batch{ID: batch.ID})
			c.controlMu.Lock()
			got := c.runEpochs["job-active"]
			c.controlMu.Unlock()
			c.cancelRun("job-active")
			c.mu.Unlock()
			if got != tc.want {
				t.Fatalf("launch epoch = %d, want %d; persisted claims must belong to an older run", got, tc.want)
			}
		})
	}
}
