package database

import (
	"encoding/json"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"go.uber.org/zap"
)

func learningFixture(t *testing.T) (*DB, string, string) {
	t.Helper()
	db := newRBACTestDB(t)
	if err := db.BootstrapRBAC("test-hash", map[string]string{"chat:read": "read", "project:read": "read"}); err != nil {
		t.Fatal(err)
	}
	u, err := db.CreateRBACUser("learner", "learner", "test-hash", true, []string{RBACSystemRoleAdmin})
	if err != nil {
		t.Fatal(err)
	}
	c, err := db.CreateConversation("synthetic task", ConversationCreateMeta{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SetResourceOwner("conversation", c.ID, u.ID); err != nil {
		t.Fatal(err)
	}
	return db, c.ID, u.ID
}
func learningRun(t *testing.T, db *DB, cid, id string) *Message {
	t.Helper()
	if _, err := db.AddMessage(cid, "user", "test tool option compatibility", nil); err != nil {
		t.Fatal(err)
	}
	msg, err := db.AddMessage(cid, "assistant", "checked synthetic output", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.BeginLearningRun(id, cid); err != nil {
		t.Fatal(err)
	}
	return msg
}

func TestLearningRunBoundariesAndIdempotentFinish(t *testing.T) {
	db, cid, _ := learningFixture(t)
	msg := learningRun(t, db, cid, "r1")
	if err := db.AddProcessDetail(msg.ID, cid, "tool_result", "", map[string]any{"success": true, "result": "synthetic help output"}); err != nil {
		t.Fatal(err)
	}
	if err := db.BeginLearningRun("r1", cid); err != nil {
		t.Fatal(err)
	}
	if err := db.FinishLearningRun("r1", "completed"); err != nil {
		t.Fatal(err)
	}
	if err := db.FinishLearningRun("r1", "failed"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.AddMessage(cid, "user", "NEXT RUN MUST NOT LEAK", nil); err != nil {
		t.Fatal(err)
	}
	j, err := db.ClaimLearningJob(time.Now(), 20, time.Minute)
	if err != nil || j == nil {
		t.Fatalf("claim: %v %v", j, err)
	}
	if j.RunStatus != "completed" || j.Outcome != "unknown" {
		t.Fatalf("invented success or repeated finish: %+v", j)
	}
	e, err := db.LearningRunEvidence(j, 300)
	if err != nil {
		t.Fatal(err)
	}
	tool := false
	count := 0
	for _, item := range e {
		if item.Text == "NEXT RUN MUST NOT LEAK" {
			t.Fatal("mixed runs")
		}
		tool = tool || item.Kind == "tool_result"
		count += len([]rune(item.Text))
	}
	if !tool || count > 300 {
		t.Fatalf("tool evidence/budget: %#v", e)
	}
}

func TestLearningClaimLeaseAndDailyBudget(t *testing.T) {
	db, cid, _ := learningFixture(t)
	for _, id := range []string{"one", "two"} {
		learningRun(t, db, cid, id)
		if err := db.FinishLearningRun(id, "completed"); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now()
	var wg sync.WaitGroup
	wg.Add(2)
	jobs := make(chan *LearningJob, 2)
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() { defer wg.Done(); j, err := db.ClaimLearningJob(now, 1, time.Minute); jobs <- j; errs <- err }()
	}
	wg.Wait()
	close(jobs)
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	var first *LearningJob
	for j := range jobs {
		if j != nil {
			if first != nil {
				t.Fatal("daily limit exceeded by competing workers")
			}
			first = j
		}
	}
	if first == nil {
		t.Fatal("no claim")
	}
	second, err := db.ClaimLearningJob(now.Add(2*time.Minute), 1, time.Minute)
	if err != nil || second == nil || second.ID != first.ID || second.LeaseToken == first.LeaseToken {
		t.Fatalf("recovery: %+v %v", second, err)
	}
	if err := db.RetryLearningJob(first, "stale worker", now); err != nil {
		t.Fatal(err)
	}
	var token string
	if err := db.QueryRow("SELECT lease_token FROM learning_jobs WHERE id=?", second.ID).Scan(&token); err != nil || token != second.LeaseToken {
		t.Fatal("stale worker changed renewed lease")
	}
	third, err := db.ClaimLearningJob(now.Add(4*time.Minute), 1, time.Minute)
	if err != nil || third == nil || third.Attempts != 3 {
		t.Fatalf("third claim: %+v %v", third, err)
	}
	_, err = db.ClaimLearningJob(now.Add(6*time.Minute), 1, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	var status string
	if err := db.QueryRow("SELECT status FROM learning_jobs WHERE id=?", first.ID).Scan(&status); err != nil || status != "failed" {
		t.Fatal("exhausted lease not finalized")
	}
}

func TestLearningEvidenceDeletionAndSourceRevocation(t *testing.T) {
	db, cid, owner := learningFixture(t)
	msg := learningRun(t, db, cid, "draft")
	if err := db.AddProcessDetail(msg.ID, cid, "tool_result", "", map[string]any{"result": "fixture"}); err != nil {
		t.Fatal(err)
	}
	if err := db.FinishLearningRun("draft", "completed"); err != nil {
		t.Fatal(err)
	}
	j, err := db.ClaimLearningJob(time.Now(), 20, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if !db.LearningSourceValid(j) {
		t.Fatal("owned source not readable")
	}
	e, _ := db.LearningRunEvidence(j, 1000)
	var evidenceID string
	for _, v := range e {
		if v.Kind == "tool_result" {
			evidenceID = v.ID
		}
	}
	b, _ := json.Marshal(map[string]any{"evidenceIds": []string{evidenceID}})
	if err := db.CompleteLearningJob(j, "drafted", string(b), "hash", "", 5, 2); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("DELETE FROM process_details WHERE id=?", evidenceID); err != nil {
		t.Fatal(err)
	}
	jobs, err := db.ListLearningJobs(cid)
	if err != nil || len(jobs) != 1 || jobs[0].CandidateJSON != "" || jobs[0].Status != "skipped" {
		t.Fatalf("deleted evidence survived: %+v %v", jobs, err)
	}
	learningRun(t, db, cid, "revoked")
	_ = db.FinishLearningRun("revoked", "completed")
	j, err = db.ClaimLearningJob(time.Now(), 20, time.Minute)
	if err != nil || j == nil {
		t.Fatal(err)
	}
	disabled := false
	if err := db.UpdateRBACUser(owner, "learner", &disabled, nil); err != nil {
		t.Fatal(err)
	}
	if db.LearningSourceValid(j) {
		t.Fatal("disabled owner still authorized")
	}
	if err := db.CompleteLearningJob(j, "drafted", string(b), "hash", "", 0, 0); err != nil {
		t.Fatal(err)
	}
	if err := db.DeleteConversation(cid); err != nil {
		t.Fatal(err)
	}
	if err := db.RetryLearningJob(j, "late worker", time.Now()); err != nil {
		t.Fatal(err)
	}
	var n int
	_ = db.QueryRow("SELECT COUNT(*) FROM learning_jobs").Scan(&n)
	if n != 0 {
		t.Fatal("deletion resurrected draft")
	}
}

func TestLearningRestartAndProjectDeletion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "restart.db")
	db, err := NewDB(path, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	p, err := db.CreateProject(&Project{Name: "synthetic"})
	if err != nil {
		t.Fatal(err)
	}
	c, err := db.CreateConversation("test", ConversationCreateMeta{ProjectID: p.ID})
	if err != nil {
		t.Fatal(err)
	}
	_ = db.SetResourceOwner("conversation", c.ID, "test-owner")
	learningRun(t, db, c.ID, "interrupted")
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = NewDB(path, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.RecoverLearningRuns(); err != nil {
		t.Fatal(err)
	}
	jobs, err := db.ListLearningJobs(c.ID)
	if err != nil || len(jobs) != 1 || jobs[0].RunStatus != "interrupted" || jobs[0].Status != "skipped" {
		t.Fatalf("recovered false success: %+v %v", jobs, err)
	}
	if err := db.DeleteProject(p.ID); err != nil {
		t.Fatal(err)
	}
	jobs, err = db.ListLearningJobs(c.ID)
	if err != nil || len(jobs) != 0 {
		t.Fatal("project-derived experience survived deletion")
	}
}

func TestLearningNextDayBudgetAndRetention(t *testing.T) {
	db, cid, _ := learningFixture(t)
	now := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	learningRun(t, db, cid, "old")
	_ = db.FinishLearningRun("old", "completed")
	first, err := db.ClaimLearningJob(now, 1, time.Minute)
	if err != nil || first == nil {
		t.Fatal(err)
	}
	_ = db.RetryLearningJob(first, "retry", now)
	learningRun(t, db, cid, "new")
	_ = db.FinishLearningRun("new", "completed")
	second, err := db.ClaimLearningJob(now.Add(24*time.Hour), 1, time.Minute)
	if err != nil || second == nil {
		t.Fatal(err)
	}
	third, err := db.ClaimLearningJob(now.Add(24*time.Hour), 1, time.Minute)
	if err != nil || third != nil {
		t.Fatal("cross-day retry bypassed budget")
	}
	if _, err := db.Exec("UPDATE learning_jobs SET created_at=?", now.Add(-31*24*time.Hour).Unix()); err != nil {
		t.Fatal(err)
	}
	if err := db.PruneLearningJobs(now.Add(24 * time.Hour)); err != nil {
		t.Fatal(err)
	}
	var n int
	_ = db.QueryRow("SELECT COUNT(*) FROM learning_jobs").Scan(&n)
	if n != 1 {
		t.Fatal("active lease removed or expired history retained")
	}
}
