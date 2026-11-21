package openapi

// 开放 API 的权限范围。
//
// 刻意拆得很细：第三方程序往往只需要一类数据，
// 粗粒度的单一 scope 会让它被迫拿到超出需要的访问权。
const (
	ScopeReadPapers = "read:papers"
	ScopeReadPosts  = "read:posts"
	ScopeReadUsers  = "read:users"
)

// AllScopes 是全部合法 scope，用于校验用户提交的 scopes 是否拼写正确。
func AllScopes() []string {
	return []string{ScopeReadPapers, ScopeReadPosts, ScopeReadUsers}
}

// isValidScope 判断 scope 是否在已知列表内。
// 拒绝未知 scope，而不是静默忽略：拼错的 scope 会让用户以为已经授权，
// 实际调用时却收到 403，排查成本远高于创建时就报错。
func isValidScope(scope string) bool {
	for _, known := range AllScopes() {
		if scope == known {
			return true
		}
	}
	return false
}

// HasScope 判断密钥的 scopes 是否包含 required。
func HasScope(scopes []string, required string) bool {
	for _, scope := range scopes {
		if scope == required {
			return true
		}
	}
	return false
}
