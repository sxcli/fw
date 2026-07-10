//go:build windows

// scmbox is the Windows probe binary for the wine-based SCM tests: a
// single SCMApplet exercising the dual launch mode. Built on demand by
// x_scm_wine_test.go; the go tool ignores testdata for normal builds.
package main

import (
	"fmt"
	"os"

	"golang.org/x/sys/windows/svc"
	sxclifw "sxcli.dev/fw"
)

type probeConfig struct {
	Note string `json:"note" arg:"note,n" usage:"a note to print"`
	Exit int    `json:"exit" arg:"exit" usage:"exit code to return"`
}

type serviceProbe struct {
	cfg probeConfig
}

func (p *serviceProbe) Configured() error {
	fmt.Printf("probe: configured note=%s\n", p.cfg.Note)
	return nil
}

// Run is the console launch mode.
func (p *serviceProbe) Run() int {
	fmt.Printf("probe: console run note=%s\n", p.cfg.Note)
	return p.cfg.Exit
}

// Execute is the service launch mode: report Running, do the work,
// return — a service that exits by itself, which is all the harness
// needs.
func (p *serviceProbe) Execute(args []string, req <-chan svc.ChangeRequest, status chan<- svc.Status) (bool, uint32) {
	status <- svc.Status{State: svc.Running, Accepts: svc.AcceptStop | svc.AcceptShutdown}
	fmt.Printf("probe: service execute note=%s argc=%d\n", p.cfg.Note, len(args))
	return false, uint32(p.cfg.Exit)
}

func main() {
	p := &serviceProbe{}
	sxclifw.Register("svcprobe", p, sxclifw.WithConfig(&p.cfg))
	if os.Getenv("SCMBOX_DEBUG_OFF") != "1" {
		sxclifw.Enable(sxclifw.FeatureSCMDebug)
	}
	sxclifw.Main()
}
