package profile_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"reflect"
	"testing"

	"denova/internal/quality/domain"
	"denova/internal/quality/profile"
)

type walkthroughExpectation struct {
	Contract               domain.Contract
	SpecID                 string
	Revision               int
	ProfileID              domain.ProfileID
	GoalCatalogSHA256      string
	LayersSHA256           string
	CandidateChangesSHA256 string
	Walkthrough            profile.Walkthrough
	Resolution             domain.Resolution
}

func TestAcceptedADRWalkthroughsMatchCompleteResolvedStructures(t *testing.T) {
	decoder := newDecoder(t)
	profiles := loadCommittedProfiles(t, decoder)
	registry, err := profile.NewRegistry(profiles)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	for id, want := range expectedWalkthroughs() {
		id, want := id, want
		t.Run(string(id), func(t *testing.T) {
			item, err := registry.Lookup(id)
			if err != nil {
				t.Fatalf("Lookup(%q): %v", id, err)
			}
			resolved, err := domain.ResolveQualitySpec(item.QualitySpec)
			if err != nil {
				t.Fatalf("ResolveQualitySpec(%q): %v", id, err)
			}
			got := walkthroughExpectation{
				Contract:               item.QualitySpec.Contract,
				SpecID:                 item.QualitySpec.SpecID,
				Revision:               item.QualitySpec.Revision,
				ProfileID:              item.QualitySpec.ProfileID,
				GoalCatalogSHA256:      completeStructureSHA256(t, item.QualitySpec.GoalCatalog),
				LayersSHA256:           completeStructureSHA256(t, item.QualitySpec.Layers),
				CandidateChangesSHA256: completeStructureSHA256(t, item.QualitySpec.CandidateChanges),
				Walkthrough:            item.Walkthrough,
				Resolution:             resolved,
			}
			if !reflect.DeepEqual(got, want) {
				gotJSON, _ := json.MarshalIndent(got, "", "  ")
				wantJSON, _ := json.MarshalIndent(want, "", "  ")
				t.Fatalf("complete walkthrough mismatch:\n got: %s\nwant: %s", gotJSON, wantJSON)
			}
		})
	}
}

func expectedWalkthroughs() map[domain.ProfileID]walkthroughExpectation {
	return map[domain.ProfileID]walkthroughExpectation{
		domain.ProfileLongSerial: {
			Contract:               domain.Contract{Kind: domain.QualitySpecContractKind, Version: domain.ContractVersionV1, IssuedAt: "2026-07-21T09:10:00+08:00"},
			SpecID:                 "qs-long-serial-chapter-12",
			Revision:               1,
			ProfileID:              domain.ProfileLongSerial,
			GoalCatalogSHA256:      "cc24f07e6a52f8c4d2f55f68e14c2f3cbe52d7c2bf1baa5c233e57593695cd20",
			LayersSHA256:           "c696e3194f81f59faf9d9d1d2c3efdd5a69dbe18cf321eb3a9cb0906297b7145",
			CandidateChangesSHA256: "b3006f0f1288414c2e93d5b0d32a984d4bf1eb5438c2531b579a98a9c32d3500",
			Walkthrough: profile.Walkthrough{
				OperationID: "draft_chapter_12",
				ArtifactRef: "chapters/0012.md",
				Description: domain.LocalizedText{
					ZhCN: "为长篇连载起草第 12 章，并以已确认的严格连续性合同检查人物状态、伏笔与章节推进。",
					En:   "Draft chapter 12 of a long serial and check character state, setups, and progress under the confirmed strict-continuity contract.",
				},
				EvaluationFocus: []string{"qg.serial.continuity", "qg.serial.chapter_end_momentum"},
			},
			Resolution: expectedResolution("2026-07-21T09:14:00+08:00", []domain.ResolvedGoal{
				resolvedGoal("qg.serial.continuity", "strict", domain.LayerProjectOverrides, "confirm-op-serial-ch12",
					step(domain.LayerProfileDefaults, "normal", provenance("profile-long-serial-v1", "profile_contract", "long_serial default", "2026-07-21", "2026-07-21", "2026-07-21T09:10:00+08:00")),
					step(domain.LayerProjectOverrides, "strict", provenance("project-ashen-river-contract", "project_contract", "CREATOR.md#continuity", "2026-07-20", "2026-07-20", "2026-07-21T09:11:00+08:00"))),
				resolvedGoal("qg.serial.chapter_end_momentum", "high", domain.LayerTaskOverrides, "confirm-op-serial-ch12",
					step(domain.LayerProfileDefaults, "medium", provenance("profile-long-serial-v1", "profile_contract", "long_serial default", "2026-07-21", "2026-07-21", "2026-07-21T09:10:00+08:00")),
					step(domain.LayerTaskOverrides, "high", provenance("task-chapter-12-draft", "task_contract", "setting/chapter-12-plan.md", "2026-07-21", "2026-07-21", "2026-07-21T09:12:00+08:00"))),
			}),
		},
		domain.ProfileFanqieShort: {
			Contract:               domain.Contract{Kind: domain.QualitySpecContractKind, Version: domain.ContractVersionV1, IssuedAt: "2026-07-21T10:10:00+08:00"},
			SpecID:                 "qs-fanqie-golden-opening",
			Revision:               1,
			ProfileID:              domain.ProfileFanqieShort,
			GoalCatalogSHA256:      "316b886ba4e8ed7f0815eb99de449aa3d391f63c6083df491d12837724e0be78",
			LayersSHA256:           "b3675e3b144a64410ee6a9050b8b3918a8798b07274b5a861e765e3dd059f25d",
			CandidateChangesSHA256: "03e75144fb47d702ed57b27ae6fbc69e93128664a557ea68a64a69e6d8954e60",
			Walkthrough: profile.Walkthrough{
				OperationID: "evaluate_golden_opening",
				ArtifactRef: "artifact:fanqie-opening-golden-001",
				Description: domain.LocalizedText{
					ZhCN: "评估番茄风格短篇的黄金开篇，要求读者立即理解主角欲望、故事卖点与当下压力。",
					En:   "Evaluate a Fanqie-style golden opening so the reader immediately understands the protagonist's desire, the premise payoff, and current pressure.",
				},
				EvaluationFocus: []string{"qg.short.opening_clarity", "qg.short.hook_intensity"},
			},
			Resolution: expectedResolution("2026-07-21T10:13:00+08:00", []domain.ResolvedGoal{
				resolvedGoal("qg.short.opening_clarity", "clear", domain.LayerProfileDefaults, "confirm-op-fanqie-opening",
					step(domain.LayerProfileDefaults, "clear", provenance("profile-fanqie-short-v1", "profile_contract", "fanqie_short default", "2026-07-21", "2026-07-21", "2026-07-21T10:10:00+08:00"))),
				resolvedGoal("qg.short.hook_intensity", "high", domain.LayerTaskOverrides, "confirm-op-fanqie-opening",
					step(domain.LayerProfileDefaults, "medium", provenance("profile-fanqie-short-v1", "profile_contract", "fanqie_short default", "2026-07-21", "2026-07-21", "2026-07-21T10:10:00+08:00")),
					step(domain.LayerTaskOverrides, "high", provenance("task-golden-opening-evaluation", "task_contract", "artifact:fanqie-opening-golden-001", "2026-07-21", "2026-07-21", "2026-07-21T10:11:00+08:00"))),
			}),
		},
		domain.ProfileZhihuSaltShort: {
			Contract:               domain.Contract{Kind: domain.QualitySpecContractKind, Version: domain.ContractVersionV1, IssuedAt: "2026-07-21T11:10:00+08:00"},
			SpecID:                 "qs-zhihu-reversal-ending",
			Revision:               1,
			ProfileID:              domain.ProfileZhihuSaltShort,
			GoalCatalogSHA256:      "a143fb5d5c5799eaa253c0604ed3c056dd01ae82f568eb4de7c08fe15cc1db6a",
			LayersSHA256:           "4e109574020382fd0884a3bce25f66f5178c1575530998d3031220784e084955",
			CandidateChangesSHA256: "4f53cda18c2baa0c0354bb5f9a3ecbe5ed12ab4d8e11ba873c2f11161202b945",
			Walkthrough: profile.Walkthrough{
				OperationID: "evaluate_reversal_ending",
				ArtifactRef: "artifact:zhihu-reversal-ending-001",
				Description: domain.LocalizedText{
					ZhCN: "评估知乎盐选风格短篇的反转结尾，要求反转能回溯到前文证据并完成因果闭环。",
					En:   "Evaluate a Zhihu Salt-style reversal ending whose turn traces back to earlier evidence and closes the causal chain.",
				},
				EvaluationFocus: []string{"qg.short.reversal_evidence", "qg.short.causal_closure"},
			},
			Resolution: expectedResolution("2026-07-21T11:13:00+08:00", []domain.ResolvedGoal{
				resolvedGoal("qg.short.reversal_evidence", "strict", domain.LayerProjectOverrides, "confirm-op-zhihu-reversal",
					step(domain.LayerProfileDefaults, "traceable", provenance("profile-zhihu-salt-v1", "profile_contract", "zhihu_salt_short default", "2026-07-21", "2026-07-21", "2026-07-21T11:10:00+08:00")),
					step(domain.LayerProjectOverrides, "strict", provenance("project-reversal-red-line", "project_contract", "CREATOR.md#reversal-evidence", "2026-07-20", "2026-07-20", "2026-07-21T11:11:00+08:00"))),
				resolvedGoal("qg.short.causal_closure", "closed", domain.LayerTaskOverrides, "confirm-op-zhihu-reversal",
					step(domain.LayerProfileDefaults, "open", provenance("profile-zhihu-salt-v1", "profile_contract", "zhihu_salt_short default", "2026-07-21", "2026-07-21", "2026-07-21T11:10:00+08:00")),
					step(domain.LayerTaskOverrides, "closed", provenance("task-reversal-ending-evaluation", "task_contract", "artifact:zhihu-reversal-ending-001", "2026-07-21", "2026-07-21", "2026-07-21T11:11:30+08:00"))),
			}),
		},
	}
}

func expectedResolution(validatedAt string, goals []domain.ResolvedGoal) domain.Resolution {
	return domain.Resolution{
		MergeOrder:                      domain.MergeOrder(),
		UnknownOrUnsupportedValuePolicy: domain.RejectExplicitly,
		ValidatorContract:               domain.ResolutionValidatorV1,
		ValidatedAt:                     validatedAt,
		ResolvedGoals:                   goals,
	}
}

func completeStructureSHA256(t *testing.T, value any) string {
	t.Helper()
	payload, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal complete walkthrough structure: %v", err)
	}
	var document any
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.UseNumber()
	if err := decoder.Decode(&document); err != nil {
		t.Fatalf("decode complete walkthrough structure: %v", err)
	}
	canonical, err := json.Marshal(document)
	if err != nil {
		t.Fatalf("canonicalize complete walkthrough structure: %v", err)
	}
	sum := sha256.Sum256(canonical)
	return fmt.Sprintf("%x", sum)
}

func resolvedGoal(id string, value any, winner domain.Layer, confirmationID string, chain ...domain.ResolutionStep) domain.ResolvedGoal {
	return domain.ResolvedGoal{GoalID: id, Value: value, WinningLayer: winner, ProvenanceChain: chain, AuthorConfirmationID: confirmationID}
}

func step(layer domain.Layer, value any, source domain.Provenance) domain.ResolutionStep {
	return domain.ResolutionStep{Layer: layer, Value: value, Provenance: source}
}

func provenance(sourceID, sourceKind, sourceRef, observedAt, effectiveFrom, recordedAt string) domain.Provenance {
	return domain.Provenance{SourceID: sourceID, SourceKind: sourceKind, SourceRef: sourceRef, ObservedAt: observedAt, EffectiveFrom: effectiveFrom, RecordedAt: recordedAt}
}
