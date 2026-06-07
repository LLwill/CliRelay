package auth

import cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/executor"

type weightedCursorState struct {
	current   map[string]int
	tieCursor int
}

func ensureWeightedState(states map[string]*weightedCursorState, key string, limit int) map[string]*weightedCursorState {
	if states == nil {
		states = make(map[string]*weightedCursorState)
	}
	if _, ok := states[key]; !ok && len(states) >= limit {
		states = make(map[string]*weightedCursorState)
	}
	if _, ok := states[key]; !ok {
		states[key] = &weightedCursorState{current: make(map[string]int)}
	}
	return states
}

func weightedSelectionKey(provider, model string, opts cliproxyexecutor.Options) string {
	return provider + ":" + canonicalModelKey(model) + ":" + weightedSelectionScope(opts.Metadata)
}

func pickWeightedAvailable(states map[string]*weightedCursorState, key string, available []*Auth) *Auth {
	if len(available) == 0 {
		return nil
	}
	state := states[key]
	if state == nil {
		state = &weightedCursorState{current: make(map[string]int)}
		states[key] = state
	}
	if state.current == nil {
		state.current = make(map[string]int)
	}

	activeIDs := make(map[string]struct{}, len(available))
	totalWeight := 0
	for _, auth := range available {
		if auth == nil {
			continue
		}
		activeIDs[auth.ID] = struct{}{}
		weight := authSelectionWeight(auth)
		if weight <= 0 {
			continue
		}
		totalWeight += weight
		state.current[auth.ID] += weight
	}
	for id := range state.current {
		if _, ok := activeIDs[id]; !ok {
			delete(state.current, id)
		}
	}
	if totalWeight <= 0 {
		return nil
	}

	start := 0
	if len(available) > 0 {
		start = state.tieCursor % len(available)
	}
	bestIndex := -1
	bestScore := 0
	for offset := 0; offset < len(available); offset++ {
		index := (start + offset) % len(available)
		score := state.current[available[index].ID]
		if bestIndex == -1 || score > bestScore {
			bestIndex = index
			bestScore = score
		}
	}
	if bestIndex < 0 {
		bestIndex = 0
	}
	selected := available[bestIndex]
	state.current[selected.ID] -= totalWeight
	if state.tieCursor >= 2_147_483_640 {
		state.tieCursor = 0
	}
	state.tieCursor++
	return selected
}
