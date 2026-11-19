package agents

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/ubgo/agentio"
	"github.com/ubgo/agentio/gemini"
	"github.com/ubgo/agentio/opencode"
	"github.com/ubgo/agentio/opencode/store"
	"github.com/ubgo/agentio/pi"
)

// opencodeExtraPrefix namespaces opencode keys in an entry's raw JSON.
const opencodeExtraPrefix = "opencode."

// dbKeySep joins a database path and a session id into a Transcript.Key.
const dbKeySep = "#"

// geminiAgent is Gemini CLI: conversations and turns through agentio.
func geminiAgent(g *gemini.Agent) Agent {
	n := g.Neutral()
	read := neutralReader(n)
	return Agent{
		Tool: ToolGemini, Neutral: n, TestedMax: gemini.TestedMax, UseTitle: true,
		transcripts: func(ctx context.Context, h agentio.Home) ([]Transcript, error) {
			_, out, err := conversationTranscripts(ctx, n, h, read)
			return out, err
		},
	}
}

// piAgent is pi: sessions and turns through agentio.
func piAgent(p *pi.Agent) Agent {
	n := p.Neutral()
	read := neutralReader(n)
	return Agent{
		Tool: ToolPi, Neutral: n, TestedMax: pi.TestedMax, UseTitle: true,
		transcripts: func(ctx context.Context, h agentio.Home) ([]Transcript, error) {
			_, out, err := conversationTranscripts(ctx, n, h, read)
			return out, err
		},
	}
}

// opencodeAgent is opencode: the legacy JSON storage tree (sessions
// written up to 1.1.x, one session file per transcript) and the
// opencode.db of 1.2+ (one database-held transcript per session). The
// legacy tree is listed first, so a session present in both is matched by
// message id when the database copy is read.
func opencodeAgent(o *opencode.Agent) Agent {
	n := o.Neutral()
	read := neutralReader(n)
	return Agent{
		Tool: ToolOpencode, Neutral: n, TestedMax: opencode.TestedMax, UseTitle: true,
		transcripts: func(ctx context.Context, h agentio.Home) ([]Transcript, error) {
			oh, err := o.HomeOf(h)
			if err != nil {
				return nil, err
			}
			_, legacy, err := conversationTranscripts(ctx, n, h, read)
			if err != nil {
				return nil, err
			}
			for i := range legacy {
				t := &legacy[i]
				project := ""
				if s, err := o.Session(oh, t.ConversationID); err == nil {
					project = s.ProjectID
				}
				t.annotate = func() Annotator { return opencodeAnnotator(project) }
			}
			db, err := opencodeDBTranscripts(ctx, o, oh)
			return append(legacy, db...), err
		},
	}
}

// opencodeDBTranscripts lists the sessions of h's opencode.db; none when
// the home has no database file (a pre-1.2 install). Disk · Read.
func opencodeDBTranscripts(ctx context.Context, o *opencode.Agent, h opencode.Home) ([]Transcript, error) {
	db, err := store.OpenHome(o, h)
	if errors.Is(err, store.ErrNoDatabase) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(db.Path()); err != nil {
		return nil, nil
	}
	// Read-only database: a close error cannot lose data.
	defer func() { _ = db.Close() }()
	ss, err := db.Sessions(ctx, store.SessionQuery{Roots: true, Active: true})
	if err != nil {
		return nil, fmt.Errorf("opencode: %s: %w", db.Path(), err)
	}
	path := db.Path()
	out := make([]Transcript, 0, len(ss))
	for _, s := range ss {
		c := s.Conversation(h)
		sid, project := s.ID, s.ProjectID
		out = append(out, Transcript{
			Key: path + dbKeySep + sid, ConversationID: sid, Title: c.Title, Dir: c.Dir, Home: c.Home, Updated: c.Updated,
			read: func(from agentio.Offset, fn func(agentio.Turn) error, _ func(agentio.BadLine)) (agentio.Offset, error) {
				d := store.Open(h, path)
				// Read-only database: a close error cannot lose data.
				defer func() { _ = d.Close() }()
				return d.Turns(ctx, sid, from, fn)
			},
			annotate: func() Annotator { return opencodeAnnotator(project) },
		})
	}
	return out, nil
}

// opencodeAnnotator maps an opencode turn to the keys ailog has always
// stored for it (provider, agent, mode, project, cost) and the bare model
// id, decoded from the message JSON in Turn.Raw by agentio's
// opencode.DecodeMessage. Pure.
func opencodeAnnotator(project string) Annotator {
	return func(t agentio.Turn) Annotation {
		m, err := opencode.DecodeMessage(t.Raw)
		if err != nil {
			return Annotation{Extras: opencodeExtras("", "", "", project, 0)}
		}
		model, provider := m.ModelID, m.ProviderID
		if model == "" && m.Model != nil {
			model, provider = m.Model.ModelID, m.Model.ProviderID
		}
		a := Annotation{Extras: opencodeExtras(provider, m.Agent, m.Mode, project, m.Cost)}
		if t.Role == agentio.RoleAssistant {
			a.Model = model
		}
		return a
	}
}

// opencodeExtras packs the non-empty opencode keys; nil when all are
// empty. Pure.
func opencodeExtras(provider, agent, mode, project string, cost float64) map[string]any {
	m := map[string]any{}
	add := func(k, v string) {
		if v != "" {
			m[opencodeExtraPrefix+k] = v
		}
	}
	add("provider_id", provider)
	add("agent", agent)
	add("mode", mode)
	add("project_id", project)
	if cost > 0 {
		m[opencodeExtraPrefix+"cost_usd"] = cost
	}
	if len(m) == 0 {
		return nil
	}
	return m
}
