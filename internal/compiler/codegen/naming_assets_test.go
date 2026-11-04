package codegen

import "testing"

func TestAssetSymbolInNamingPrecheck(t *testing.T) {
	with, err := generatedPackageSymbolsWithAssets("home.gxml", 0, true, true)
	if err != nil {
		t.Fatal(err)
	}
	if !containsString(with, "goxmlAssetsHome") {
		t.Errorf("含资产的生成符号缺少 goxmlAssetsHome: %v", with)
	}
	without, err := generatedPackageSymbolsWithAssets("home.gxml", 0, true, false)
	if err != nil {
		t.Fatal(err)
	}
	if containsString(without, "goxmlAssetsHome") {
		t.Errorf("无资产时不应申报 goxmlAssetsHome: %v", without)
	}
}

func containsString(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}
