package watchadapter

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestCoordR1CLIGenerateCancellationIsBounded(t *testing.T) {
	realGo, e := exec.LookPath("go")
	if e != nil {
		t.Fatal(e)
	}
	root := t.TempDir()
	proxyDir := filepath.Join(root, "proxy")
	os.MkdirAll(proxyDir, 0700)
	src := filepath.Join(proxyDir, "proxy.go")
	bin := filepath.Join(proxyDir, "go.exe")
	proxySource := `package main
import("os";"os/exec";"time")
func main(){
 marker:=os.Getenv("COORD_GO_ENTERED");release:=os.Getenv("COORD_GO_RELEASE")
 os.WriteFile(marker,[]byte("entered"),0600)
 deadline:=time.Now().Add(8*time.Second)
 for {if _,e:=os.Stat(release);e==nil{break};if time.Now().After(deadline){os.Exit(88)};time.Sleep(time.Millisecond)}
 cmd:=exec.Command(os.Getenv("COORD_REAL_GO"),os.Args[1:]...);cmd.Stdout=os.Stdout;cmd.Stderr=os.Stderr
 if e:=cmd.Run();e!=nil{os.Exit(1)}
}
`
	os.WriteFile(src, []byte(proxySource), 0600)
	build := exec.Command(realGo, "build", "-o", bin, src)
	if out, e := build.CombinedOutput(); e != nil {
		t.Fatalf("proxy build: %v %s", e, out)
	}
	proj := filepath.Join(root, "project")
	os.MkdirAll(proj, 0700)
	os.WriteFile(filepath.Join(proj, "go.mod"), []byte("module example.com/coord-cancel\n\ngo 1.26.0\n"), 0600)
	os.WriteFile(filepath.Join(proj, "home.gxml"), []byte(`<Goxml Version="1"><Page><Text Value="static"/></Page></Goxml>`), 0600)
	entered := filepath.Join(root, "entered")
	release := filepath.Join(root, "release")
	t.Setenv("COORD_GO_ENTERED", entered)
	t.Setenv("COORD_GO_RELEASE", release)
	t.Setenv("COORD_REAL_GO", realGo)
	t.Setenv("PATH", proxyDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	defer os.WriteFile(release, []byte("release"), 0600)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan int, 1)
	go func() { rc, _ := CLIGenerate(ctx, proj); done <- rc }()
	deadline := time.After(5 * time.Second)
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for {
		if _, e := os.Stat(entered); e == nil {
			break
		}
		select {
		case <-deadline:
			t.Fatal("real cli.Run did not reach owned blocking Go proxy")
		case <-tick.C:
		}
	}
	cancel()
	timely := false
	select {
	case <-done:
		timely = true
	case <-time.After(time.Second):
	}
	os.WriteFile(release, []byte("release"), 0600)
	if !timely {
		select {
		case <-done:
		case <-time.After(15 * time.Second):
			t.Fatal("adapter did not join after controlled proxy release")
		}
	}
	if !timely {
		t.Fatal("CLIGenerate remained blocked after cancellation until Go proxy was externally released; cli.Run had no cancellation input")
	}
}
