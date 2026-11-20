package tushare

import "strings"

// Endpoint 描述一个 tushare 接口的元数据。
type Endpoint struct {
	// Name 是服务端 api_name，如 daily。
	Name string
	// Method 是对应的 Client 方法名，如 Daily。
	Method string
	// Title 是接口中文名，如「A股日线行情」。
	Title string
	// Category 是文档分类路径，如「股票数据/行情数据/历史日线」。
	Category string
	// DocURL 是官方文档地址。
	DocURL string
	// RequiredParams 列出必填参数名。
	RequiredParams []string
}

// Endpoints 返回全部接口元数据（按官方文档顺序），供 CLI、文档站或 AI Agent 检索。
//
// 返回的切片为只读，请勿修改。
func Endpoints() []Endpoint { return endpointSpecs }

// LookupEndpoint 按 api_name 或 Go 方法名查找接口元数据（忽略大小写）。
func LookupEndpoint(name string) (Endpoint, bool) {
	key := strings.ToLower(strings.TrimSpace(name))
	if key == "" {
		return Endpoint{}, false
	}
	for _, e := range endpointSpecs {
		if strings.ToLower(e.Name) == key || strings.ToLower(e.Method) == key {
			return e, true
		}
	}
	return Endpoint{}, false
}

//go:generate go run ./cmd/tusharegen -spec spec/endpoints.json -out . -docs docs/endpoints.md
