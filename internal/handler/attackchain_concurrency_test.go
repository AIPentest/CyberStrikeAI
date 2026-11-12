package handler

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"

	"cyberstrike-ai/internal/database"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

func TestBusyAttackChainRegenerationPreservesCachedGraph(t *testing.T) {
	db, err := database.NewDB(filepath.Join(t.TempDir(), "chain.db"), zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	conv, err := db.CreateConversation("cached", database.ConversationCreateMeta{})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"target", "action"} {
		if err := db.SaveAttackChainNode(conv.ID, id, id, "saved "+id, "", `{}`, 0); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.SaveAttackChainEdge(conv.ID, "edge", "target", "action", "leads_to", 1); err != nil {
		t.Fatal(err)
	}
	h := NewAttackChainHandler(db, nil, zap.NewNop())
	lock := &sync.Mutex{}
	lock.Lock() // Another generation is still writing this conversation's graph.
	defer lock.Unlock()
	h.generatingLocks.Store(conv.ID, lock)
	router := gin.New()
	router.POST("/api/attack-chain/:conversationId/regenerate", h.RegenerateAttackChain)
	for i := 0; i < 2; i++ {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/attack-chain/"+conv.ID+"/regenerate", nil))
		if recorder.Code != http.StatusConflict {
			t.Fatalf("busy regeneration returned %d: %s", recorder.Code, recorder.Body.String())
		}
		nodes, err := db.LoadAttackChainNodes(conv.ID)
		if err != nil {
			t.Fatal(err)
		}
		edges, err := db.LoadAttackChainEdges(conv.ID)
		if err != nil {
			t.Fatal(err)
		}
		if len(nodes) != 2 || len(edges) != 1 || edges[0].Source != "target" || edges[0].Target != "action" {
			t.Fatalf("409 request modified the graph being generated: nodes=%+v edges=%+v", nodes, edges)
		}
	}
}
