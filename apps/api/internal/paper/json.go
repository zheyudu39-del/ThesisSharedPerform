package paper

import "encoding/json"

// jsonUnmarshal 单独抽出来便于测试时替换为严格模式或注入失败。
func jsonUnmarshal(data []byte, v any) error {
	return json.Unmarshal(data, v)
}
