package assistant

import (
	"sync"
	"time"
)

const assistantAutoRouteTTL = 5 * time.Minute

type assistantAutoRouteEntry struct {
	modelID   string
	expiresAt time.Time
}

// This is short-lived health information observed by the backend, never a user
// preference or a prediction of remaining provider quota. Account and per-user
// quota counters live separately and are never changed by model rotation.
type assistantAutoRoutingState struct {
	sync.Mutex
	accounts map[string]assistantAutoRouteEntry
}

var openRouterAutoRouting assistantAutoRoutingState

func (state *assistantAutoRoutingState) preferred(account string, models []AssistantModel, now time.Time) string {
	state.Lock()
	defer state.Unlock()
	state.prune(now)
	entry, ok := state.accounts[account]
	if !ok {
		return ""
	}
	for _, model := range models {
		if model.ID == entry.modelID {
			return entry.modelID
		}
	}
	// A model no longer in the verified zero-priced catalog cannot be reused.
	delete(state.accounts, account)
	return ""
}

func (state *assistantAutoRoutingState) remember(account, modelID string, now time.Time) {
	state.Lock()
	defer state.Unlock()
	state.prune(now)
	if state.accounts == nil {
		state.accounts = make(map[string]assistantAutoRouteEntry)
	}
	entry, exists := state.accounts[account]
	if exists && entry.modelID == modelID {
		// A fixed expiry permits retrying the normal recommendation after five
		// minutes, rather than pinning a fallback forever during steady traffic.
		return
	}
	state.accounts[account] = assistantAutoRouteEntry{modelID: modelID, expiresAt: now.Add(assistantAutoRouteTTL)}
}

func (state *assistantAutoRoutingState) forget(account, modelID string) {
	state.Lock()
	defer state.Unlock()
	if entry, exists := state.accounts[account]; exists && entry.modelID == modelID {
		delete(state.accounts, account)
	}
}

// Called under the state mutex. No background polling or provider calls are
// needed; idle account entries disappear on the next lookup/update.
func (state *assistantAutoRoutingState) prune(now time.Time) {
	for account, entry := range state.accounts {
		if !now.Before(entry.expiresAt) {
			delete(state.accounts, account)
		}
	}
}
