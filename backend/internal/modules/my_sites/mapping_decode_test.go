package my_sites

import (
	"net/http/httptest"
	"strings"
	"testing"

	"transithub/backend/internal/shared/httpjson"
)

// 前端保存映射时会把 GET 返回的整个 mapping 对象原样回传，其中包含服务端独占写入的
// lastAutoPricingRun。历史版本的 MappingRequest 没有声明该字段，配合
// httpjson.Decode 的 DisallowUnknownFields 会让「编辑数据源」和「关闭自动调价」
// 这类请求直接 400，前端表现为 admin.mySites.errors.request。
func decodeMappingBody(t *testing.T, body string) (MappingRequest, error) {
	t.Helper()
	request := httptest.NewRequest("PATCH", "/api/my-sites/mappings", strings.NewReader(body))
	var dto struct {
		Mapping MappingRequest `json:"mapping"`
	}
	err := httpjson.Decode(request, &dto)
	return dto.Mapping, err
}

const mappingWithRunStatus = `{"mapping":{
	"ownGroup":"own-a",
	"upstreamTargets":[{"siteId":"site-1","groupName":"up-a"}],
	"enableAutoPricing":false,
	"autoPricingSource":"primary_upstream",
	"primaryUpstreamSiteId":"site-1",
	"primaryUpstreamGroupName":"up-a",
	"autoPricingStrategy":"percentage",
	"fixedIncrease":0.1,
	"percentageIncrease":10,
	"adjustThresholdPercent":10,
	"minMultiplier":null,
	"maxMultiplier":null,
	"enableAutoPricingNotify":false,
	"autoPricingNotifyBotIds":[],
	"autoPricingNotifyTemplate":"",
	"lastAutoPricingRun":{
		"status":"applied",
		"trigger":"manual",
		"ranAt":"2024-01-01T00:00:00Z",
		"oldReference":1,
		"newReference":2,
		"oldOwnMultiplier":1,
		"newOwnMultiplier":2,
		"targetMultiplier":2
	}
}}`

func TestDecodeMappingRequestAcceptsServerOwnedRunStatus(t *testing.T) {
	mapping, err := decodeMappingBody(t, mappingWithRunStatus)
	if err != nil {
		t.Fatalf("decode round-tripped mapping: %v", err)
	}
	if mapping.OwnGroup != "own-a" {
		t.Fatalf("ownGroup = %q, want own-a", mapping.OwnGroup)
	}
	if mapping.EnableAutoPricing {
		t.Fatal("enableAutoPricing = true, want false: disabling auto-pricing must survive decoding")
	}
}

// 关闭自动调价时，即使请求仍带着旧的主上游和执行状态，归一化也必须成功，
// 因为主上游校验只在 EnableAutoPricing 为 true 时才适用。
func TestNormalizeMappingRequestAllowsDisablingAutoPricing(t *testing.T) {
	mapping, err := decodeMappingBody(t, mappingWithRunStatus)
	if err != nil {
		t.Fatalf("decode round-tripped mapping: %v", err)
	}
	normalized, include, err := normalizeMappingRequest(mapping)
	if err != nil {
		t.Fatalf("normalize disabled auto-pricing mapping: %v", err)
	}
	if !include {
		t.Fatal("include = false, want true")
	}
	if normalized.EnableAutoPricing {
		t.Fatal("normalized EnableAutoPricing = true, want false")
	}
	// 客户端回传的执行状态不可信，归一化结果必须丢弃它，由服务端状态为准。
	if normalized.LastAutoPricingRun != nil {
		t.Fatal("normalized LastAutoPricingRun must be nil; server state is authoritative")
	}
}

// 真正非法的字段仍必须被拒绝，避免为修一个 bug 而放宽整个请求校验。
func TestDecodeMappingRequestStillRejectsUnknownFields(t *testing.T) {
	if _, err := decodeMappingBody(t, `{"mapping":{"ownGroup":"own-a","totallyUnknownField":1}}`); err == nil {
		t.Fatal("expected unknown field to be rejected")
	}
}
