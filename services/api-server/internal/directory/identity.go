// Package directory は外部ディレクトリ・IdP（OIDC/LDAP/SCIM）からのユーザー情報を
// users テーブルへ反映する共通のプロビジョニング基盤を提供する。
//
// 認証（AuthN: 誰がログインしたか）と権限属性の真実の源（AuthZ/profile: role・
// manager 等）は別の関心事として扱う。OIDC の groups claim は LDAP/SCIM ディレクトリ
// 同期が無い環境向けのフォールバックであり、いずれのソースも本パッケージの
// Provisioner を通して同じ経路で users テーブルへ反映される
// （権威の優先順位 manual > ldap/scim > oidc は repository.UpsertFederatedUser 側で解決）。
package directory

import "github.com/koizumib/mailshield/services/api-server/internal/domain"

// ExternalIdentity は外部ディレクトリ・IdP から得られる、プロビジョニング前に
// 正規化されたユーザー情報を表す。
//
// Role の解決（グループ→ロールのマッピング等）は呼び出し側（各ソース固有のロジック。
// 例: GroupRoleMapper）が行い、ここには解決済みの値を渡す。ソースによってロールの
// 表現方法が異なりうるため（OIDC/LDAP はグループ経由、SCIM は専用属性経由になる
// 可能性がある）、Provisioner 自体はマッピング方式に関知しない。
type ExternalIdentity struct {
	Email       string
	DisplayName string
	Role        domain.Role
	Source      domain.ProvisionedBy
}
