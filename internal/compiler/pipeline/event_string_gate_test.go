package pipeline

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/goxml/goxml/internal/compiler/analyzer"
	"github.com/goxml/goxml/internal/compiler/codegen"
)

func TestMalformedEventStringStopsBeforeCodegen(t *testing.T) {
	for _, handler := range []string{`RecordText('a,b)`, `RecordText('a,b',)`, `RecordText(, 'a,b')`} {
		t.Run(handler, func(t *testing.T) {
			dir := t.TempDir()
			target := filepath.Join(dir, "dashboard_gen.go")
			previous := []byte("package demo\n// previous generated output\n")
			if err := os.WriteFile(target, previous, 0o644); err != nil {
				t.Fatal(err)
			}
			loads, generations := 0, 0
			src := []byte(`<Goxml Version="1"><Page><Button Text="choose" Click="` + handler + `"/></Page></Goxml>`)
			result := Run("dashboard.gxml", src, Config{SourceDir: dir, PackageName: "demo", SourcePath: "dashboard.gxml"}, Hooks{
				Load: func(Config) (*analyzer.TypeEnv, error) {
					loads++
					return nil, errors.New("unexpected type loading after invalid event")
				},
				Generate: func(*analyzer.VerifiedDoc, codegen.Options) ([]byte, error) {
					generations++
					return []byte("package demo\n"), nil
				},
			})
			if loads != 0 || generations != 0 || result.Artifact != nil || result.Status != analyzer.StatusBlocked {
				t.Fatalf("failed event reached later stages: loads=%d generations=%d result=%+v", loads, generations, result)
			}
			if len(result.Diags) != 1 || result.Diags[0].Code != "GXML-EVENT-005" {
				t.Fatalf("want one GXML-EVENT-005 diagnostic, got %v", result.Diags)
			}
			entries, err := os.ReadDir(dir)
			if err != nil || len(entries) != 1 {
				t.Fatalf("failed pipeline changed directory: entries=%v err=%v", entries, err)
			}
			current, err := os.ReadFile(target)
			if err != nil || !bytes.Equal(current, previous) {
				t.Fatalf("failed pipeline changed previous output: %q, %v", current, err)
			}
		})
	}
}
