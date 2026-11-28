package data

import "testing"

func TestCanonicalIndex(t *testing.T) {
	if got := CanonicalIndex(" 399300.sz "); got != "000300.SH" {
		t.Fatalf("沪深300 深市代码应归一为 000300.SH，得到 %s", got)
	}
	if got := CanonicalIndex("000688.SH"); got != "000688.SH" {
		t.Fatalf("科创50 应保持不变，得到 %s", got)
	}
	if CanonicalIndex("  ") != "" {
		t.Fatal("空代码应仍为空")
	}
}

func TestUniversesUnique(t *testing.T) {
	if len(Universes) < 8 {
		t.Fatalf("常用股票池过少：%d", len(Universes))
	}
	seen := map[string]bool{}
	need := map[string]bool{
		AllMarket:   false, // 全市场
		"000300.SH": false, // 沪深300
		"000905.SH": false, // 中证500
		"000688.SH": false, // 科创50
		"399006.SZ": false, // 创业板指
	}
	for _, u := range Universes {
		if u.Code == "" || u.Name == "" || u.Group == "" {
			t.Fatalf("股票池条目不完整：%+v", u)
		}
		if seen[u.Code] {
			t.Fatalf("重复指数代码 %s", u.Code)
		}
		seen[u.Code] = true
		if _, ok := need[u.Code]; ok {
			need[u.Code] = true
		}
	}
	for code, ok := range need {
		if !ok {
			t.Fatalf("缺少常用指数 %s", code)
		}
	}
}

func TestUniverseName(t *testing.T) {
	if UniverseName("399300.SZ") != "沪深300" {
		t.Fatalf("399300.SZ 应显示为沪深300，得到 %s", UniverseName("399300.SZ"))
	}
	if UniverseName("399006.SZ") != "创业板指" {
		t.Fatalf("创业板指名称不对：%s", UniverseName("399006.SZ"))
	}
	if UniverseName(AllMarket) != "全市场" {
		t.Fatalf("ALL 应显示为全市场，得到 %s", UniverseName(AllMarket))
	}
}
