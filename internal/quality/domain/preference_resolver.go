package domain

import (
	"sort"
	"time"
)

type PreferenceQuery struct {
	AuthorID    string
	ProjectID   string
	WorkspaceID string
	Dimension   string
}

type PreferenceSuppression struct {
	SignalID   string
	BySignalID string
	Reason     string
}

type PreferenceResolution struct {
	Effective  *PreferenceSignal
	Applicable []PreferenceSignal
	Suppressed []PreferenceSuppression
	Reason     string
}

// ResolvePreferences applies workspace -> project -> author precedence, then
// strength, recorded_at, and lexical signal_id tie-breaks deterministically.
func ResolvePreferences(signals []PreferenceSignal, query PreferenceQuery) (PreferenceResolution, error) {
	if err := ValidatePreferenceJournal(signals); err != nil {
		return PreferenceResolution{}, err
	}
	suppressedBy := make(map[string]PreferenceSuppression)
	for _, signal := range signals {
		for _, target := range signal.SupersedesSignalIDs {
			suppressedBy[target] = PreferenceSuppression{SignalID: target, BySignalID: signal.SignalID, Reason: "superseded_by_correction"}
		}
		for _, target := range signal.RevokesSignalIDs {
			suppressedBy[target] = PreferenceSuppression{SignalID: target, BySignalID: signal.SignalID, Reason: "revoked_by_author"}
		}
	}
	result := PreferenceResolution{Applicable: make([]PreferenceSignal, 0), Suppressed: make([]PreferenceSuppression, 0)}
	for _, suppression := range suppressedBy {
		result.Suppressed = append(result.Suppressed, suppression)
	}
	sort.Slice(result.Suppressed, func(i, j int) bool { return result.Suppressed[i].SignalID < result.Suppressed[j].SignalID })

	bestScope := 0
	for _, signal := range signals {
		if signal.Author.ActorID != query.AuthorID || signal.Preference.Dimension != query.Dimension || signal.Event == "revocation" {
			continue
		}
		if _, suppressed := suppressedBy[signal.SignalID]; suppressed {
			continue
		}
		if !preferenceApplicable(signal, query) {
			continue
		}
		rank := preferenceScopeRank(signal.Scope.Kind)
		if rank > bestScope {
			bestScope = rank
			result.Applicable = result.Applicable[:0]
		}
		if rank == bestScope {
			result.Applicable = append(result.Applicable, signal)
		}
	}
	if len(result.Applicable) == 0 {
		result.Reason = "no_applicable_explicit_author_signal"
		return result, nil
	}
	sort.Slice(result.Applicable, func(i, j int) bool {
		return preferenceLess(result.Applicable[i], result.Applicable[j])
	})
	winner := result.Applicable[len(result.Applicable)-1]
	result.Effective = &winner
	result.Reason = "scope_then_strength_then_recorded_at_then_signal_id"
	return result, nil
}

func preferenceApplicable(signal PreferenceSignal, query PreferenceQuery) bool {
	if signal.Scope.AuthorID != query.AuthorID {
		return false
	}
	switch signal.Scope.Kind {
	case "workspace":
		return signal.Scope.ProjectID == query.ProjectID && signal.Scope.WorkspaceID == query.WorkspaceID
	case "project":
		return signal.Scope.ProjectID == query.ProjectID
	case "author":
		return true
	}
	return false
}

func preferenceLess(left, right PreferenceSignal) bool {
	leftStrength := preferenceStrength(left.Preference.Strength)
	rightStrength := preferenceStrength(right.Preference.Strength)
	if leftStrength != rightStrength {
		return leftStrength < rightStrength
	}
	leftTime, _ := time.Parse(time.RFC3339, left.RecordedAt)
	rightTime, _ := time.Parse(time.RFC3339, right.RecordedAt)
	if !leftTime.Equal(rightTime) {
		return leftTime.Before(rightTime)
	}
	return left.SignalID < right.SignalID
}

func preferenceStrength(value string) int {
	switch value {
	case "weak":
		return 1
	case "normal":
		return 2
	case "strong":
		return 3
	}
	return 0
}
