package control

import "testing"

func TestValidName(t *testing.T) {
	ok := []string{"01_uv", "a", "x-y_z", "0", "abcdefghijabcdefghijabcdefghijabcdefghijabcdefghijabcdefghijabcd"}
	for _, n := range ok {
		if !ValidName(n) {
			t.Errorf("ValidName(%q) = false, want true", n)
		}
	}

	ng := []string{
		"", "_a", "-a", "A", "a.kage", "a/b", "../x", "a b", "日本語",
		"abcdefghijabcdefghijabcdefghijabcdefghijabcdefghijabcdefghijabcde", // 65 文字
	}
	for _, n := range ng {
		if ValidName(n) {
			t.Errorf("ValidName(%q) = true, want false", n)
		}
	}
}

func TestNameFromPath(t *testing.T) {
	cases := map[string]string{
		"/abs/shaders/01_uv.kage": "01_uv",
		"shaders/x.kage":          "x",
		"y.kage":                  "y",
	}
	for in, want := range cases {
		if got := NameFromPath(in); got != want {
			t.Errorf("NameFromPath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestHasUnitPixels(t *testing.T) {
	yes := []string{
		"//kage:unit pixels\npackage main\n",
		"package main\n//kage:unit pixels\n",
		"  //kage:unit pixels  \r\npackage main",
	}
	for _, s := range yes {
		if !HasUnitPixels(s) {
			t.Errorf("HasUnitPixels(%q) = false, want true", s)
		}
	}

	no := []string{
		"package main\n",
		"//kage:unit texels\npackage main\n",
		"// //kage:unit pixels\n",
		"x := 1 //kage:unit pixels\n",
	}
	for _, s := range no {
		if HasUnitPixels(s) {
			t.Errorf("HasUnitPixels(%q) = true, want false", s)
		}
	}
}
