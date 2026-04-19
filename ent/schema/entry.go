package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type Entry struct {
	ent.Schema
}

func (Entry) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "entries"},
	}
}

func (Entry) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").
			Unique().
			Immutable().
			NotEmpty(),

		field.String("tool").Default(""),

		field.String("cwd").Default(""),
		field.String("project").Default(""),
		field.String("repo_owner").Default(""),
		field.String("repo_name").Default(""),
		field.String("repo_remote").Default(""),
		field.String("git_branch").Default(""),
		field.String("git_commit").Default(""),

		field.String("session_id").Default(""),
		field.String("session_name").Default(""),
		field.Int("turn_index").Default(0),
		field.String("parent_entry_id").Default(""),

		field.String("hostname").Default(""),
		field.String("user").Default(""),
		field.String("shell").Default(""),
		field.String("terminal").Default(""),
		field.String("terminal_title").Default(""),
		field.String("tty").Default(""),
		field.Int("pid").Default(0),

		field.Text("prompt").Default(""),
		field.Text("response").Default(""),
		field.String("model").Default(""),
		field.Text("raw").Default(""),
		field.Int("token_count_in").Default(0),
		field.Int("token_count_out").Default(0),

		field.String("tags").Default(""),
		field.Bool("starred").Default(false),
		field.Text("notes").Default(""),

		field.Time("created_at").Default(time.Now).Immutable(),
	}
}

func (Entry) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("project", "created_at"),
		index.Fields("tool", "created_at"),
		index.Fields("session_id", "turn_index"),
		index.Fields("cwd", "created_at"),
		index.Fields("git_branch", "created_at"),
		index.Fields("starred"),
	}
}
