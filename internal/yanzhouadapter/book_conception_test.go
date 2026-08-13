package yanzhouadapter

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"denova/internal/yanzhouprotocol"
)

func canonicalConceptionFixture() map[string]any {
	settings := []map[string]any{}
	for _, category := range []string{"世界规则与限制", "力量体系", "重要阵营", "关键地点", "核心资源"} {
		settings = append(settings, map[string]any{
			"category": category, "introduction": category + "直接约束主线推进",
			"items": []map[string]any{
				{"name": category + "一", "introduction": "使用一次就会失去一段私人记忆"},
				{"name": category + "二", "introduction": "只能在午夜退潮后的十分钟内生效"},
				{"name": category + "三", "introduction": "被全城共同记住后便永久失效"},
			},
		})
	}
	roles := []string{"主角", "关键配角", "关键配角", "反派", "关键配角", "反派阵营内应"}
	names := []string{"林彻", "顾雨", "闻秋", "周衡", "许灯", "韩默"}
	characters := make([]map[string]any, 0, len(roles))
	for index, role := range roles {
		characters = append(characters, map[string]any{
			"id": "char-" + strconv.Itoa(index+1), "name": names[index],
			"role": role, "biography": "与主角共同追查失踪者，并在信任与背叛之间改变关系",
			"desire": "让被遗忘的人重新被记住", "boundary": []string{"不牺牲无辜者"},
			"cost": []string{"每次行动都会失去一段自己的记忆"},
		})
	}
	volumes := make([]map[string]any, 0, 4)
	volumeNames := []string{"第一卷", "第二卷", "第三卷", "第四卷"}
	for index := 1; index <= 4; index++ {
		start := 1
		end := 10
		status := "detailed"
		if index > 1 {
			start = 11 + (index-2)*20
			end = start + 19
			status = "skeleton"
		}
		volumes = append(volumes, map[string]any{
			"planId": "vp-" + strconv.Itoa(index), "planVersion": 1, "status": status,
			"volumeName": volumeNames[index-1], "objective": "查明第一个被全城遗忘的人",
			"entryState": []string{"主角只记得妹妹留下的黑伞"}, "exitState": []string{"主角掌握记忆交易证据"},
			"escalation": []string{"私人失踪升级为全城共谋"}, "conflictChain": []string{"修复旧物", "追查交易", "公开证据"},
			"climax": "主角在钟楼广播被抹去者的姓名", "closure": "妹妹短暂出现并留下下一卷线索",
			"characterTurns": []string{"主角从独自承担转为相信同伴"}, "threadTargets": []string{"黑伞契约"},
			"plannedChapterRange": map[string]any{"start": start, "end": end}, "sourceBookPlanVersion": 1,
		})
	}
	chapters := make([]map[string]any, 0, 10)
	for index := 1; index <= 10; index++ {
		chapterNumber := strconv.Itoa(index)
		chapterName := "第" + chapterNumber + "章 失物编号" + chapterNumber
		openingPhase := 0
		if index <= 3 {
			openingPhase = index
		}
		chapters = append(chapters, map[string]any{
			"planId": "cp-" + chapterNumber, "planVersion": 1, "branchId": "main",
			"chapterKey": "第一卷/" + chapterName, "volumePlanId": "vp-1", "volumeName": "第一卷",
			"chapterName": chapterName, "chapterOrdinal": index, "status": "planned", "openingPhase": openingPhase,
			"openingHook": "第" + chapterNumber + "件旧物叫出一个不存在的名字",
			"goal":        "确认失踪者与记忆交易的第" + chapterNumber + "条联系",
			"conflict":    "主角必须在记忆继续消失前取得证物", "stakes": "失败会让同伴忘记主角",
			"conflictEscalation": "追查范围从个人旧物扩大到市政档案", "emotionalBeat": "怀疑转为共同承担",
			"expectedChanges": []string{"获得一条不可撤回的新证据"},
			"scenes": []map[string]any{
				{"purpose": "读取旧物记忆", "opposition": "记忆被人为剪断", "turn": "黑伞留下坐标", "outcome": "找到下一名证人"},
				{"purpose": "保护证人", "opposition": "反派提前清除档案", "turn": "同伴公开身份", "outcome": "团队关系发生不可逆变化"},
			},
			"appearingRoles": []string{"角色甲", "角色乙", "角色丁"}, "coreScene": "雨夜档案馆对峙",
			"corePayoff": "主角拿到能够验证失踪者存在的实物", "endingHook": "妹妹的声音从黑伞里叫出主角真名",
			"note": "旧物线索、人物选择和章末压力连续推进", "factConstraints": []string{"记忆能力必须付出私人记忆代价"},
			"sourceBookPlanVersion": 1, "sourceVolumePlanVersion": 1,
		})
	}
	return map[string]any{
		"schemaVersion": 1,
		"meta": map[string]any{
			"bookNameCandidates": []string{"雨夜失物招领处", "全城忘了她", "黑伞记忆局"},
			"bookName":           "雨夜失物招领处", "recommendedBookName": "雨夜失物招领处", "type": "都市悬疑",
			"intro":       "开头标记-" + strings.Repeat("潮声", 7000),
			"targetWords": 800000, "marketing": "面向喜欢都市怪谈与情感悬疑的成年读者",
			"toneNote": "克制、潮湿、逐层逼近", "risksToAvoid": []string{"万能能力", "无代价反转"},
		},
		"settings": settings, "characters": characters,
		"bookPlan": map[string]any{
			"planId": "bp-main", "planVersion": 1, "status": "draft", "readerPromise": "每卷解开一层城市遗忘机制并推动兄妹重逢",
			"premise": "主角修复旧物时能听见被抹除者的记忆", "protagonistEngine": map[string]any{
				"desire": "找回妹妹", "fear": "自己也忘记妹妹", "misbelief": "只有独自承担才能保护同伴", "troubleEngine": "每次读物都会暴露新的被遗忘者",
			},
			"coreConflict": "找回妹妹需要公开全城参与的记忆交易", "stages": []map[string]any{
				{"id": "stage-1", "title": "追索", "objective": "确认交易存在", "irreversibleResult": "主角被全城监控", "startVolumeId": "vp-1", "endVolumeId": "vp-1"},
				{"id": "stage-2", "title": "反击", "objective": "联合被遗忘者", "irreversibleResult": "城市秩序公开分裂", "startVolumeId": "vp-2", "endVolumeId": "vp-3"},
				{"id": "stage-3", "title": "记住", "objective": "终止记忆交易", "irreversibleResult": "所有人承担恢复记忆的代价", "startVolumeId": "vp-4", "endVolumeId": "vp-4"},
			},
			"endingDirection": "全城重新记住被抹除者，兄妹在不完整记忆中重建关系", "immutableFacts": []string{"记忆能力必须付出私人记忆代价"},
		},
		"volumePlans": volumes, "currentVolumePlanId": "vp-1", "chapterPlans": chapters,
	}
}

func TestBookConceiveAcceptsEmptyPreBookContext(t *testing.T) {
	contextPackRef := "sha256:" + strings.Repeat("a", 64)
	result, err := json.Marshal(map[string]any{
		"kind": "read-result", "mutationPerformed": false,
		"data": map[string]any{"contextPackRef": contextPackRef, "sections": []any{}},
	})
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(map[string]any{
		"schemaVersion": "1", "toolId": "story.get_target", "success": true,
		"result": json.RawMessage(result),
	})
	if err != nil {
		t.Fatal(err)
	}
	contextText, err := decodeWritingContextResponse(yanzhouprotocol.Envelope{Payload: payload}, planRunRequest{
		CapabilityID:   "book.conceive",
		ContextPackRef: planContentRef{Ref: contextPackRef},
	})
	if err != nil {
		t.Fatalf("pre-book conception should not require a current chapter: %v", err)
	}
	if contextText != "" {
		t.Fatalf("empty pre-book context = %q, want empty", contextText)
	}
}

func TestBookConceptionInstructionBoundsCanonicalJSON(t *testing.T) {
	instruction := writingConceptionInstruction(WritingHarnessStage{RoleID: HarnessRolePrimaryWriter})
	if !strings.Contains(instruction, "under 14,000 Unicode characters") || !strings.Contains(instruction, `{"schemaVersion":1,"meta":{}`) {
		t.Fatal("book conception prompt must bound the complete canonical JSON below the observed truncation size")
	}
}

func TestBookConceiveUsesRequestedWallTime(t *testing.T) {
	profile, err := writingHarnessProfile("novel-standard")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := profile.Budget.MaxWallTimeMS, 120_000; got != want {
		t.Fatalf("standard wall time = %d, want %d", got, want)
	}
	request := planRunRequest{CapabilityID: "book.conceive", Budgets: planRunBudget{MaxWallTimeMS: 300_000}}
	if got, want := writingRunWallTimeMS(request, profile), 300_000; got != want {
		t.Fatalf("book conception wall time = %d, want requested window %d", got, want)
	}
	request.CapabilityID = "chapter.generate_from_outline"
	if got, want := writingRunWallTimeMS(request, profile), 120_000; got != want {
		t.Fatalf("chapter wall time = %d, want profile budget %d", got, want)
	}
}

func TestBookConceiveEmitsCompleteCanonicalConceptionArtifact(t *testing.T) {
	candidateJSON, err := json.Marshal(canonicalConceptionFixture())
	if err != nil {
		t.Fatal(err)
	}
	if len(candidateJSON) <= 16*1024 {
		t.Fatalf("conception fixture must reproduce the former 16KB truncation: %d", len(candidateJSON))
	}
	calls := 0
	requestBodies := [][]byte{}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		calls++
		body, _ := io.ReadAll(request.Body)
		requestBodies = append(requestBodies, body)
		content := string(candidateJSON)
		if calls == 2 {
			content = `{"status":"pass","issues":[{"path":"bookPlan.stages.1","problem":"中段转折缺少主角主动承担代价的选择","fix":"让主角公开一份会损害自身信誉的证据"},{"path":"chapterPlans.9.endingHook","problem":"第十章钩子与第一卷收束重复","fix":"把钩子改成妹妹主动留下下一卷坐标"}]}`
		}
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(map[string]any{"choices": []map[string]any{{"message": map[string]any{"role": "assistant", "content": content}, "finish_reason": "stop"}}})
	}))
	defer server.Close()

	store, err := NewFileRuntimeEventStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	runtime, err := NewWritingFrameRuntime(store, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	runID := "book-conceive-run"
	contextPackRef := "sha256:" + strings.Repeat("f", 64)
	emptyContext, err := json.Marshal(map[string]any{
		"schemaVersion": "1", "toolId": "story.get_target", "success": true,
		"result": map[string]any{
			"kind": "read-result", "mutationPerformed": false,
			"data": map[string]any{"contextPackRef": contextPackRef, "sections": []any{}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.HandleToolResponse(yanzhouprotocol.Envelope{
		Kind: yanzhouprotocol.KindToolResponse, ProtocolVersion: yanzhouprotocol.ProtocolVersion,
		RequestID: "tool-" + runID + "-context", RunID: runID, Seq: 1, Payload: emptyContext,
	}); err != nil {
		t.Fatal(err)
	}
	payload := bookConceivePayload(t, server.URL, runID, contextPackRef)
	var output bytes.Buffer
	if err := runtime.HandleFrame(context.Background(), yanzhouprotocol.Envelope{
		Kind: yanzhouprotocol.KindRunStart, ProtocolVersion: yanzhouprotocol.ProtocolVersion,
		RequestID: "request-" + runID, RunID: runID, Payload: payload,
	}, &output); err != nil {
		t.Fatal(err)
	}

	events, err := store.ReplayAfter(context.Background(), runID, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	artifactKinds := []any{}
	var pending strings.Builder
	finalConception := ""
	reviewerContent := ""
	proposalReady := false
	for _, event := range events {
		switch event.Type {
		case RunEventTypeModelDelta:
			pending.WriteString(event.Payload["text"].(string))
		case RunEventTypeArtifactCreated:
			artifactKinds = append(artifactKinds, event.Payload["artifactKind"])
			if event.Payload["artifactKind"] == "conception" {
				finalConception = pending.String()
			} else if event.Payload["artifactKind"] == "review" {
				reviewerContent = pending.String()
			}
			pending.Reset()
		case RunEventTypeProposalReady:
			proposalReady = true
		}
	}
	if calls != 2 || !reflect.DeepEqual(artifactKinds, []any{"conception", "review", "report"}) {
		t.Fatalf("calls=%d artifactKinds=%#v", calls, artifactKinds)
	}
	if !proposalReady || events[len(events)-1].Type != RunEventTypeRunCompleted {
		t.Fatalf("proposal/terminal events=%#v", events)
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(finalConception), &decoded); err != nil {
		t.Fatalf("final conception is not JSON: %v", err)
	}
	if len(decoded["settings"].([]any)) != 5 || len(decoded["characters"].([]any)) != 6 || len(decoded["volumePlans"].([]any)) != 4 || len(decoded["chapterPlans"].([]any)) != 10 {
		t.Fatalf("incomplete canonical conception: %#v", decoded)
	}
	if !bytes.Contains(requestBodies[0], []byte("complete Yanzhou canonical Book Start Package")) || !bytes.Contains(requestBodies[0], []byte("完整起笔对话")) {
		t.Fatalf("conception request lost contract or saved conversation: %s", requestBodies[0])
	}
	hasCandidateHead := bytes.Contains(requestBodies[1], []byte("开头标记-"))
	hasCandidateTail := bytes.Contains(requestBodies[1], []byte(`\"planId\":\"cp-10\"`))
	if !hasCandidateHead || !hasCandidateTail {
		t.Fatalf("reviewer did not receive the complete conception Artifact: head=%t tail=%t requestBytes=%d", hasCandidateHead, hasCandidateTail, len(requestBodies[1]))
	}
	var review map[string]any
	if err := json.Unmarshal([]byte(reviewerContent), &review); err != nil {
		t.Fatalf("review Artifact is not JSON: %v", err)
	}
	issues, _ := review["issues"].([]any)
	if len(issues) < 2 {
		t.Fatalf("reviewer issues are not concrete: %#v", review)
	}
	var providerRequest map[string]any
	if err := json.Unmarshal(requestBodies[0], &providerRequest); err != nil {
		t.Fatal(err)
	}
	thinking, _ := providerRequest["thinking"].(map[string]any)
	responseFormat, _ := providerRequest["response_format"].(map[string]any)
	if thinking["type"] != "disabled" || responseFormat["type"] != "json_object" {
		t.Fatalf("book conception request controls=%#v %#v", thinking, responseFormat)
	}
}

func bookConceivePayload(t *testing.T, baseURL, runID, contextPackRef string) json.RawMessage {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"schemaVersion": "1", "requestId": "request-" + runID,
		"idempotencyKey": "idem-" + runID, "runId": runID,
		"sessionId": "session-" + runID, "agentKind": "ide",
		"entrypoint": "agent_chat", "capabilityId": "book.conceive",
		"userIntent":       "完整起笔对话：" + strings.Repeat("作者确认凌晨四点营业。", 600),
		"explicitContinue": false, "planMode": false,
		"selectedSkillIds": []string{}, "harnessProfile": "novel-standard",
		"target": map[string]any{
			"schemaVersion": "1", "kind": "book", "bookId": "book-1",
			"targetId": "book-1", "parentIds": []string{},
		},
		"effectiveModelProfile": map[string]any{
			"profileId": "fixture", "providerType": "openai-compatible",
			"adapterId": "openai-compatible", "baseUrl": baseURL, "model": "deepseek-v4-pro",
			"capabilities": map[string]any{"streaming": true}, "timeoutMs": 30000,
			"runtimeAuth": map[string]any{"mode": "none"},
			"resolution":  map[string]any{"source": "run"},
		},
		"contextPackRef": map[string]any{"ref": contextPackRef},
		"toolCapabilityManifest": map[string]any{
			"schemaVersion": "1", "runId": runID, "agentKind": "ide",
			"target": map[string]any{
				"schemaVersion": "1", "kind": "book", "bookId": "book-1",
				"targetId": "book-1", "parentIds": []string{},
			},
			"capabilities": []map[string]any{
				{"id": "story.get_target", "mode": "read", "maxCalls": 8, "maxResultBytes": 262144},
				{"id": "story.get_outline", "mode": "read", "maxCalls": 4, "maxResultBytes": 262144},
				{"id": "story.search_chapters", "mode": "read", "maxCalls": 4, "maxResultBytes": 262144},
				{"id": "writing.create_artifact", "mode": "propose", "maxCalls": 8, "maxResultBytes": 262144},
			},
			"deniedByDefault": true, "issuedAt": "2026-08-12T00:00:00.000Z",
		},
		"budgets": map[string]any{
			"maxModelCalls": 3, "maxToolRounds": 4, "maxDelegations": 1,
			"maxRevisionRounds": 1, "maxWallTimeMs": 30000,
			"maxInputTokens": 64000, "maxOutputTokens": 16000,
		},
		"baseRevisions": map[string]string{},
		"displayLocale": "zh-CN",
	})
	if err != nil {
		t.Fatal(err)
	}
	return payload
}
