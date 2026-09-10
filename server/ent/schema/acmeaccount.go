// ACME：acme_accounts 表 —— CA 账户注册与密钥。
package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

// ACMEAccount 对应 acme_accounts 表。
type ACMEAccount struct {
	ent.Schema
}

func (ACMEAccount) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New).Immutable(),
		// directory_url 绑定的 ACME 目录；账户与目录绑定，切换目录视为新账户。
		field.String("directory_url").NotEmpty(),
		// account_url 为 CA 侧账户 URL（注册后回填）。
		field.String("account_url").Optional().Default(""),
		field.String("email").Optional().Default(""),
		// key_encrypted 为账户私钥(pem)的 AES-GCM 密文(base64)；明文密钥永不落库/回显。
		field.Text("key_encrypted").NotEmpty(),
		// status: active / error（账户异常时停止自动签发，不自动重注册覆盖）。
		field.String("status").Default("active"),
		field.String("last_error").Optional().Default(""),
		field.Time("created_at").Immutable(),
		field.Time("updated_at"),
	}
}

func (ACMEAccount) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("directory_url"),
	}
}
