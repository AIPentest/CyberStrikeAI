package deepassessment

import (
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type Node struct {
	Boundary    bool            `json:"boundary,omitempty"`
	DetailCount int             `json:"detail_count,omitempty"`
	ID          string          `json:"id"`
	StepID      string          `json:"step_id,omitempty"`
	Kind        string          `json:"kind"`
	State       string          `json:"state"`
	Title       string          `json:"title"`
	Body        string          `json:"body"`
	Source      json.RawMessage `json:"source"`
	Round       int             `json:"updated_round"`
	UpdatedAt   time.Time       `json:"updated_at"`
}
type Edge struct {
	Source     string `json:"source"`
	Target     string `json:"target"`
	Relation   string `json:"relation"`
	Projection string `json:"projection,omitempty"`
	References int    `json:"references,omitempty"`
}
type Graph struct {
	View          string `json:"view,omitempty"`
	PrimaryCount  int    `json:"primary_count"`
	BoundaryTotal int    `json:"boundary_total"`
	BoundaryMore  int    `json:"boundary_more"`
	DetailsHidden int    `json:"details_hidden"`
	Nodes         []Node `json:"nodes"`
	Edges         []Edge `json:"edges"`
	Total         int    `json:"total"`
	Folded        int    `json:"folded"`
	NextOffset    int    `json:"next_offset"`
	HasMore       bool   `json:"has_more"`
}

func graphNode(tx *sql.Tx, task string, n Node) error {
	if len(n.Source) == 0 {
		n.Source = json.RawMessage(`{}`)
	}
	_, err := tx.Exec(`INSERT INTO deep_assessment_nodes(task_id,id,step_id,kind,state,title,body,source,updated_round,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?)
 ON CONFLICT(task_id,id) DO UPDATE SET state=excluded.state,title=excluded.title,body=excluded.body,source=excluded.source,updated_round=excluded.updated_round,updated_at=excluded.updated_at
 WHERE state<>excluded.state OR title<>excluded.title OR body<>excluded.body OR source<>excluded.source`, task, n.ID, n.StepID, n.Kind, n.State, n.Title, n.Body, string(n.Source), n.Round, time.Now().UTC())
	return err
}
func graphEdge(tx *sql.Tx, task, source, target, relation string) error {
	_, err := tx.Exec(`INSERT OR IGNORE INTO deep_assessment_edges(task_id,source_id,target_id,relation) VALUES(?,?,?,?)`, task, source, target, relation)
	return err
}
func stepNode(id string) string { return "step:" + id }

// This projection commits in the same transaction as its business state. Source
// observations and Pi summaries have independent immutable IDs and remain queryable.
func syncGraph(tx *sql.Tx, t *Task, step, kind, message string, eventID int64) error {
	if err := graphNode(tx, t.ID, Node{ID: "goal", Kind: "goal", State: t.Status, Title: t.Name, Body: t.Objective + "\nScope: " + t.Scope + "\nAcceptance: " + t.Criteria, Round: t.Rounds}); err != nil {
		return err
	}
	for i, g := range t.Guidance {
		id := fmt.Sprintf("guidance:%d", i)
		src, _ := json.Marshal(map[string]any{"provenance": "authenticated_user", "index": i})
		if err := graphNode(tx, t.ID, Node{ID: id, Kind: "guidance", State: "user", Title: Clip(g, 100), Body: g, Source: src, Round: t.Rounds}); err != nil {
			return err
		}
		if err := graphEdge(tx, t.ID, "goal", id, "guides"); err != nil {
			return err
		}
	}
	for _, st := range t.Steps {
		source, _ := json.Marshal(map[string]any{"conversation_id": st.ConversationID, "evidence": st.Evidence, "depends_on": st.DependsOn, "source_ids": st.Sources, "asset_ids": st.AssetIDs, "priority": stepPriority(st), "user_paused": st.UserPaused, "guidance_count": len(st.Guidance)})
		for _, asset := range st.AssetIDs {
			if _, err := tx.Exec("INSERT OR IGNORE INTO deep_assessment_assets(task_id,asset_id) SELECT ?,id FROM assets WHERE id=?", t.ID, asset); err != nil {
				return err
			}
		}
		if err := graphNode(tx, t.ID, Node{ID: stepNode(st.ID), StepID: st.ID, Kind: "step", State: st.Status, Title: st.Title, Body: st.Summary + "\n\nInstruction: " + st.Instruction, Source: source, Round: t.Rounds}); err != nil {
			return err
		}
		parents := st.DependsOn
		if len(parents) == 0 {
			if err := graphEdge(tx, t.ID, "goal", stepNode(st.ID), "plans"); err != nil {
				return err
			}
		}
		for _, parent := range parents {
			if err := graphEdge(tx, t.ID, stepNode(parent), stepNode(st.ID), "depends_on"); err != nil {
				return err
			}
		}
		for _, ref := range st.Sources {
			if ref == stepNode(st.ID) {
				return fmt.Errorf("step cannot cite itself as a graph source")
			}
			var found int
			if err := tx.QueryRow(`SELECT COUNT(*) FROM deep_assessment_nodes WHERE task_id=? AND id=?`, t.ID, ref).Scan(&found); err != nil {
				return err
			}
			if found == 0 {
				return fmt.Errorf("graph source %s is not in this assessment", ref)
			}
			if err := graphEdge(tx, t.ID, ref, stepNode(st.ID), "informs"); err != nil {
				return err
			}
		}
		convs := append([]string{st.ConversationID}, st.Conversations...)
		for _, conv := range convs {
			if conv != "" {
				if _, err := tx.Exec(`INSERT OR IGNORE INTO deep_assessment_sessions(task_id,step_id,conversation_id) VALUES(?,?,?)`, t.ID, st.ID, conv); err != nil {
					return err
				}
			}
		}
	}
	if eventID > 0 && kind == "intent_guidance" && step != "" {
		id := fmt.Sprintf("guidance:intent:%d", eventID)
		source, _ := json.Marshal(map[string]any{"provenance": "authenticated_user", "event_id": eventID})
		if err := graphNode(tx, t.ID, Node{ID: id, StepID: step, Kind: "guidance", State: "user", Title: Clip(message, 100), Body: message, Source: source, Round: t.Rounds}); err != nil {
			return err
		}
		if err := graphEdge(tx, t.ID, id, stepNode(step), "guides"); err != nil {
			return err
		}
	}
	if eventID > 0 && strings.TrimSpace(message) != "" && (kind == "checkpoint" || kind == "step_result" || kind == "review_gap") {
		id := fmt.Sprintf("observation:%d", eventID)
		state := "note"
		parent := "goal"
		refs := []string{}
		if st := t.Step(step); st != nil {
			state = st.Status
			parent = stepNode(step)
			refs = st.Evidence
		}
		source, _ := json.Marshal(map[string]any{"event_id": eventID, "event_kind": kind, "evidence": refs})
		if err := graphNode(tx, t.ID, Node{ID: id, StepID: step, Kind: "observation", State: state, Title: Clip(message, 100), Body: message, Source: source, Round: t.Rounds}); err != nil {
			return err
		}
		if err := graphEdge(tx, t.ID, parent, id, "records"); err != nil {
			return err
		}
		for _, ref := range refs {
			var call string
			if err := tx.QueryRow(`SELECT id FROM deep_assessment_calls WHERE task_id=? AND (id=? OR execution_id=?) AND status='completed' LIMIT 1`, t.ID, ref, ref).Scan(&call); err == nil {
				if err = graphEdge(tx, t.ID, "receipt:"+call, id, "supports"); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// PrepareGraph also backfills the step/session index for pre-graph assessments.
// Cold regions are folded by reference; original notes/receipts are never deleted.
func (s *Store) PrepareGraph(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var raw string
	if err = tx.QueryRow(`SELECT payload FROM deep_assessments WHERE id=?`, id).Scan(&raw); err != nil {
		return err
	}
	var task Task
	if err = json.Unmarshal([]byte(raw), &task); err != nil {
		return err
	}
	task.Continuous()
	if err = syncGraph(tx, &task, "", "", "", 0); err != nil {
		return err
	}
	rows, err := tx.Query(`SELECT id,step_id,tool,result,execution_id,status FROM deep_assessment_calls WHERE task_id=? AND NOT EXISTS(SELECT 1 FROM deep_assessment_nodes n WHERE n.task_id=deep_assessment_calls.task_id AND n.id='receipt:'||deep_assessment_calls.id)`, id)
	if err != nil {
		return err
	}
	var receipts []Node
	for rows.Next() {
		var call, step, tool, body, execution, state string
		if err = rows.Scan(&call, &step, &tool, &body, &execution, &state); err != nil {
			rows.Close()
			return err
		}
		src, _ := json.Marshal(map[string]any{"call_id": call, "execution_id": execution})
		receipts = append(receipts, Node{ID: "receipt:" + call, StepID: step, Kind: "receipt", State: state, Title: tool, Body: body, Source: src, Round: task.Rounds})
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, n := range receipts {
		if err = graphNode(tx, id, n); err != nil {
			return err
		}
		if err = graphEdge(tx, id, stepNode(n.StepID), n.ID, "executes"); err != nil {
			return err
		}
	}
	nodes, edges, err := readGraph(tx, id)
	if err != nil {
		return err
	}
	hot := hotGraph(nodes, edges)
	active := map[string]bool{}
	for _, n := range nodes {
		if n.Kind != "digest" {
			continue
		}
		var source struct {
			Version int `json:"archive_version"`
		}
		_ = json.Unmarshal(n.Source, &source)
		if source.Version != 2 && n.State != "superseded" {
			// Preserve old archive IDs and membership for existing links, but stop
			// treating unrelated ID batches as a single current branch.
			n.State = "superseded"
			if err = graphNode(tx, id, n); err != nil {
				return err
			}
		}
		if source.Version == 2 && n.State == "folded" {
			active[n.ID] = true
		}
	}
	covered := map[string]bool{}
	for _, edge := range edges {
		if edge.Relation == "contains" && active[edge.Source] {
			covered[edge.Target] = true
		}
	}
	var cold []Node
	stamps := map[string]int{}
	stampRows, stampErr := tx.Query("SELECT node_id,since_round FROM deep_assessment_cold WHERE task_id=?", id)
	if stampErr != nil {
		return stampErr
	}
	for stampRows.Next() {
		var key string
		var round int
		if stampErr = stampRows.Scan(&key, &round); stampErr != nil {
			stampRows.Close()
			return stampErr
		}
		stamps[key] = round
	}
	stampErr = stampRows.Err()
	stampRows.Close()
	if stampErr != nil {
		return stampErr
	}
	for _, n := range nodes {
		if n.Kind != "step" {
			continue
		}
		if (n.State == "completed" || n.State == "retired") && !hot[n.ID] {
			since, ok := stamps[n.ID]
			if !ok || since > task.Rounds {
				since = task.Rounds
				if _, err = tx.Exec("INSERT INTO deep_assessment_cold(task_id,node_id,since_round) VALUES(?,?,?) ON CONFLICT(task_id,node_id) DO UPDATE SET since_round=excluded.since_round", id, n.ID, since); err != nil {
					return err
				}
			}
			if !covered[n.ID] && task.Rounds-since >= 6 {
				cold = append(cold, n)
			}
		} else if _, ok := stamps[n.ID]; ok {
			if _, err = tx.Exec("DELETE FROM deep_assessment_cold WHERE task_id=? AND node_id=?", id, n.ID); err != nil {
				return err
			}
		}
	}
	for _, members := range coldBranches(cold, nodes, edges) {
		ids := []string{}
		var summary strings.Builder
		for _, member := range members {
			ids = append(ids, member.ID)
			fmt.Fprintf(&summary, "[%s] %s · %s\n%s\n", member.ID, member.State, member.Title, Clip(member.Body, 240))
		}
		sum := sha256.Sum256([]byte("branch-v2\x00" + strings.Join(ids, "\x00")))
		digest := fmt.Sprintf("digest:%x", sum[:12])
		src, _ := json.Marshal(map[string]any{"archive_version": 2, "members": ids, "member_count": len(ids), "provenance": "connected-branch reference index; original records and Pi summaries retained"})
		if err = graphNode(tx, id, Node{ID: digest, Kind: "digest", State: "folded", Title: fmt.Sprintf("历史分支 · %d", len(ids)), Body: summary.String(), Source: src, Round: task.Rounds}); err != nil {
			return err
		}
		if err = graphEdge(tx, id, "goal", digest, "archives"); err != nil {
			return err
		}
		for _, member := range members {
			if err = graphEdge(tx, id, digest, member.ID, "contains"); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

type graphQuery interface {
	Query(string, ...any) (*sql.Rows, error)
}

func readGraph(db graphQuery, id string) ([]Node, []Edge, error) {
	rows, err := db.Query(`SELECT id,step_id,kind,state,title,substr(body,1,600),source,updated_round,updated_at FROM deep_assessment_nodes WHERE task_id=? ORDER BY updated_at DESC,id`, id)
	if err != nil {
		return nil, nil, err
	}
	nodes := []Node{}
	for rows.Next() {
		var n Node
		var src string
		if err = rows.Scan(&n.ID, &n.StepID, &n.Kind, &n.State, &n.Title, &n.Body, &src, &n.Round, &n.UpdatedAt); err != nil {
			rows.Close()
			return nil, nil, err
		}
		n.Source = json.RawMessage(src)
		nodes = append(nodes, n)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, nil, err
	}
	rows, err = db.Query(`SELECT source_id,target_id,relation FROM deep_assessment_edges WHERE task_id=? ORDER BY source_id,target_id,relation`, id)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	edges := []Edge{}
	for rows.Next() {
		var e Edge
		if err = rows.Scan(&e.Source, &e.Target, &e.Relation); err != nil {
			return nil, nil, err
		}
		edges = append(edges, e)
	}
	return nodes, edges, rows.Err()
}

// Active directions keep their dependency and evidence ancestors visible. Digest
// containment is a presentation relation and must not affect causal reachability.
func hotGraph(nodes []Node, edges []Edge) map[string]bool {
	parents := map[string][]string{}
	for _, e := range edges {
		if e.Relation != "contains" {
			parents[e.Target] = append(parents[e.Target], e.Source)
		}
	}
	queue := []string{}
	for _, n := range nodes {
		if n.Kind == "step" && n.State != "completed" && n.State != "retired" {
			queue = append(queue, n.ID)
		}
	}
	hot := map[string]bool{"goal": true}
	for len(queue) > 0 {
		id := queue[len(queue)-1]
		queue = queue[:len(queue)-1]
		if hot[id] {
			continue
		}
		hot[id] = true
		queue = append(queue, parents[id]...)
	}
	return hot
}

func (s *Store) Graph(id, scope string, limit, offset int) (Graph, error) {
	return s.GraphVisible(id, scope, limit, offset, nil)
}

func (s *Store) GraphVisible(id, scope string, limit, offset int, allow func(Node) bool) (Graph, error) {
	return s.GraphViewVisible(id, scope, "", limit, offset, allow)
}

func (s *Store) Node(id, nodeID string, offset int) (map[string]any, error) {
	if offset < 0 {
		return nil, fmt.Errorf("invalid offset")
	}
	var n Node
	var src string
	err := s.DB.QueryRow(`SELECT id,step_id,kind,state,title,substr(body,?,8192),source,updated_round,updated_at FROM deep_assessment_nodes WHERE task_id=? AND id=?`, offset+1, id, nodeID).Scan(&n.ID, &n.StepID, &n.Kind, &n.State, &n.Title, &n.Body, &src, &n.Round, &n.UpdatedAt)
	if err != nil {
		return nil, err
	}
	n.Source = json.RawMessage(src)
	var size int
	if err = s.DB.QueryRow(`SELECT length(body) FROM deep_assessment_nodes WHERE task_id=? AND id=?`, id, nodeID).Scan(&size); err != nil {
		return nil, err
	}
	next := offset + len([]rune(n.Body))
	return map[string]any{"node": n, "next_offset": next, "has_more": next < size}, nil
}

func (s *Store) SavePiSummary(task, step, sessionFile, entryID, summary string) error {
	if entryID == "" || strings.TrimSpace(summary) == "" {
		return nil
	}
	// Include the session path in the hash: native entry IDs are session-local.
	sum := sha256.Sum256([]byte(sessionFile + "\x00" + entryID))
	id := fmt.Sprintf("memory:%x", sum[:16])
	source, _ := json.Marshal(map[string]any{"session_entry_id": entryID, "provenance": "pi_compaction; model-generated summary, not independently verified"})
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var raw string
	if err = tx.QueryRow(`SELECT payload FROM deep_assessments WHERE id=?`, task).Scan(&raw); err != nil {
		return err
	}
	var t Task
	if err = json.Unmarshal([]byte(raw), &t); err != nil {
		return err
	}
	if err = syncGraph(tx, &t, "", "", "", 0); err != nil {
		return err
	}
	if err = graphNode(tx, task, Node{ID: id, StepID: step, Kind: "memory", State: "summary", Title: "Pi 会话压缩摘要", Body: summary, Source: source, Round: t.Rounds}); err != nil {
		return err
	}
	parent := "goal"
	if t.Step(step) != nil {
		parent = stepNode(step)
	}
	if err = graphEdge(tx, task, parent, id, "summarizes"); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) SearchNodes(task, query, step string, offset int) ([]Node, error) {
	if offset < 0 {
		return nil, fmt.Errorf("invalid offset")
	}
	rows, err := s.DB.Query(`SELECT id,step_id,kind,state,title,substr(body,max(1,instr(lower(body),lower(?))-200),1600),source,updated_round,updated_at FROM deep_assessment_nodes WHERE task_id=? AND kind<>'finding' AND NOT(kind='digest' AND state='superseded') AND (?='' OR step_id=?) AND (instr(lower(title),lower(?))>0 OR instr(lower(body),lower(?))>0) ORDER BY updated_at DESC,id LIMIT 20 OFFSET ?`, query, task, step, step, query, query, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Node{}
	for rows.Next() {
		var n Node
		var src string
		if err = rows.Scan(&n.ID, &n.StepID, &n.Kind, &n.State, &n.Title, &n.Body, &src, &n.Round, &n.UpdatedAt); err != nil {
			return nil, err
		}
		n.Source = json.RawMessage(src)
		out = append(out, n)
	}
	return out, rows.Err()
}
func (s *Store) ValidateSources(task string, steps []Step) error {
	for _, st := range steps {
		for _, ref := range st.Sources {
			var n int
			if err := s.DB.QueryRow(`SELECT COUNT(*) FROM deep_assessment_nodes WHERE task_id=? AND id=?`, task, ref).Scan(&n); err != nil {
				return err
			}
			if n == 0 {
				return fmt.Errorf("unknown graph source: %s", ref)
			}
		}
	}
	return nil
}
