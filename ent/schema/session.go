package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type Session struct {
	ent.Schema
}

func (Session) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "sessions"},
	}
}

func (Session) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").
			Unique().
			Immutable().
			NotEmpty(),

		field.String("tool").Default(""),
		field.String("project").Default(""),
		field.String("session_name").Default(""),

		field.Time("started_at").Default(time.Now).Immutable(),
		field.Time("ended_at").Optional().Nillable(),
		field.Int("entry_count").Default(0),
	}
}

func (Session) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("project", "started_at"),
		index.Fields("tool", "started_at"),
	}
}
