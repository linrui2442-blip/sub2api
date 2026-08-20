package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/Wei-Shaw/sub2api/ent/schema/mixins"
	"github.com/Wei-Shaw/sub2api/internal/domain"
)

// Group is a private routing and authorization boundary.
type Group struct{ ent.Schema }

func (Group) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "groups"}}
}

func (Group) Mixin() []ent.Mixin {
	return []ent.Mixin{mixins.TimeMixin{}, mixins.SoftDeleteMixin{}}
}

func (Group) Fields() []ent.Field {
	return []ent.Field{
		field.String("name").MaxLen(100).NotEmpty(),
		field.String("description").Optional().Nillable().SchemaType(map[string]string{dialect.Postgres: "text"}),
		field.Bool("is_exclusive").Default(false),
		field.String("status").MaxLen(20).Default(domain.StatusActive),
		field.String("duplicate_operation_id").MaxLen(64).Optional().Nillable().Immutable(),
		field.String("platform").MaxLen(50).Default(domain.PlatformAnthropic),
		field.Bool("claude_code_only").Default(false),
		field.Int64("fallback_group_id").Optional().Nillable(),
		field.Int64("fallback_group_id_on_invalid_request").Optional().Nillable(),
		field.JSON("model_routing", map[string][]int64{}).Optional().SchemaType(map[string]string{dialect.Postgres: "jsonb"}),
		field.Bool("model_routing_enabled").Default(false),
		field.Bool("mcp_xml_inject").Default(true),
		field.JSON("supported_model_scopes", []string{}).Default([]string{"claude", "gemini_text", "gemini_image"}).SchemaType(map[string]string{dialect.Postgres: "jsonb"}),
		field.Int("sort_order").Default(0),
		field.Bool("allow_messages_dispatch").Default(false),
		field.Bool("allow_live").Default(false),
		field.Bool("require_oauth_only").Default(false),
		field.Bool("require_privacy_set").Default(false),
		field.String("default_mapped_model").MaxLen(100).Default(""),
		field.JSON("messages_dispatch_model_config", domain.OpenAIMessagesDispatchModelConfig{}).Default(domain.OpenAIMessagesDispatchModelConfig{}).SchemaType(map[string]string{dialect.Postgres: "jsonb"}),
		field.JSON("models_list_config", domain.GroupModelsListConfig{}).Default(domain.GroupModelsListConfig{}).SchemaType(map[string]string{dialect.Postgres: "jsonb"}),
		field.Int("rpm_limit").Default(0),
		field.String("max_reasoning_effort").MaxLen(20).Default(""),
		field.JSON("reasoning_effort_mappings", []domain.ReasoningEffortMapping{}).Default([]domain.ReasoningEffortMapping{}).SchemaType(map[string]string{dialect.Postgres: "jsonb"}),
	}
}

func (Group) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("api_keys", APIKey.Type),
		edge.To("usage_logs", UsageLog.Type),
		edge.From("accounts", Account.Type).Ref("groups").Through("account_groups", AccountGroup.Type),
		edge.From("allowed_users", User.Type).Ref("allowed_groups").Through("user_allowed_groups", UserAllowedGroup.Type),
	}
}

func (Group) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("status"), index.Fields("platform"), index.Fields("is_exclusive"),
		index.Fields("deleted_at"), index.Fields("sort_order"),
		index.Fields("duplicate_operation_id").Unique().StorageKey("idx_groups_duplicate_operation_id_active").Annotations(entsql.IndexWhere("duplicate_operation_id IS NOT NULL AND deleted_at IS NULL")),
	}
}
