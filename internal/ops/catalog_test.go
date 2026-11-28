package ops

import "testing"

func TestInfosCoversRegistryWithoutKeywords(t *testing.T) {
	infos := Infos()
	if len(infos) == 0 {
		t.Fatal("算子目录为空")
	}
	seen := map[string]bool{}
	for _, info := range infos {
		if info.Name == "and" || info.Name == "or" || info.Name == "not" {
			t.Fatalf("语言关键字不应出现在目录: %s", info.Name)
		}
		if info.Category == "" || info.Signature == "" || info.Description == "" || info.Example == "" || info.Tip == "" {
			t.Fatalf("%s 注册时漏写说明（category/signature/description/example/tip）", info.Name)
		}
		seen[info.Name] = true
	}
	for name, op := range Registry {
		if op.Doc.Keyword {
			if seen[name] {
				t.Fatalf("关键字 %s 不应出现在 Infos()", name)
			}
			continue
		}
		if !seen[name] {
			t.Fatalf("已注册算子 %s 未出现在 Infos()", name)
		}
	}
}
