package codegen

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestReviewR11InjectionChild(t *testing.T) {
	mode := os.Getenv("R11_REVIEW_INJECTION")
	if mode == "" {
		return
	}
	gen := "package demo\n type View struct {\n\tstaticCached0 bool\n}\nfunc (v *View) x(){\n\t\tv.staticCached0 = true\n}\n"
	if mode == "field" {
		gen = strings.Replace(gen, "\tstaticCached0 bool\n", "", 1)
	}
	if mode == "increment" {
		gen = strings.Replace(gen, "\t\tv.staticCached0 = true\n", "", 1)
	}
	if mode == "duplicate" {
		gen = strings.Replace(gen, "\tstaticCached0 bool\n", "\tstaticCached0 bool\n\tstaticCached0 bool\n", 1)
	}
	if mode == "production" {
		gen += "// staticRecs0\n"
	}
	got := injectProbeRecs(t, gen, 1)
	if strings.Count(got, "probeRecs0 int") != 1 || strings.Count(got, "v.probeRecs0++") != 1 {
		t.Fatal("injection absent")
	}
	fmt.Println("R11-REVIEW-INJECTION-DONE")
}
func TestReviewR11InjectionClosed(t *testing.T) {
	for _, mode := range []string{"healthy", "field", "increment", "duplicate", "production"} {
		t.Run(mode, func(t *testing.T) {
			cmd := exec.Command(os.Args[0], "-test.v", "-test.run=^TestReviewR11InjectionChild$")
			cmd.Env = append(os.Environ(), "R11_REVIEW_INJECTION="+mode)
			b, e := cmd.CombinedOutput()
			if mode == "healthy" {
				if e != nil || !strings.Contains(string(b), "R11-REVIEW-INJECTION-DONE") {
					t.Fatalf("healthy %v: %s", e, b)
				}
			} else {
				if e == nil || !strings.Contains(string(b), "--- FAIL") {
					t.Fatalf("bad anchor accepted: %s", b)
				}
			}
		})
	}
}
