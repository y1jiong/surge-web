package service

import (
	"fmt"
	"os"

	"github.com/kardianos/service"
)

type program struct {
	exit    chan struct{}
	runFunc func()
}

func (p *program) Start(s service.Service) error {
	p.exit = make(chan struct{})

	go func() {
		defer close(p.exit)
		p.runFunc()
	}()

	return nil
}

func (p *program) Stop(s service.Service) error {
	if p.exit != nil {
		<-p.exit
	}
	return nil
}

// New creates a service configured to run the given function.
// extraArgs are appended to the binary's command line when the service manager starts it.
func New(name, displayName, description string, extraArgs []string, runFunc func()) (service.Service, error) {
	cfg := &service.Config{
		Name:        name,
		DisplayName: displayName,
		Description: description,
		Arguments:   append([]string{"run"}, extraArgs...),
	}
	return service.New(&program{runFunc: runFunc}, cfg)
}

// RunCommand executes a service management action (install, uninstall, start, stop, status).
func RunCommand(s service.Service, action string) {
	var svcErr error
	switch action {
	case "install":
		svcErr = s.Install()
	case "uninstall":
		_ = s.Stop()
		svcErr = s.Uninstall()
	case "start":
		svcErr = s.Start()
	case "stop":
		svcErr = s.Stop()
	case "status":
		status, err := s.Status()
		if err != nil {
			svcErr = err
		} else {
			switch status {
			case service.StatusRunning:
				fmt.Println("service is running")
			case service.StatusStopped:
				fmt.Println("service is stopped")
			default:
				fmt.Println("service is not installed or status unknown")
			}
		}
	default:
		fmt.Fprintf(os.Stderr, "unknown service action: %s\n", action)
		os.Exit(1)
	}

	if svcErr != nil {
		fmt.Fprintf(os.Stderr, "service %s failed: %v\n", action, svcErr)
		os.Exit(1)
	}
	if action != "status" {
		fmt.Printf("service %s completed successfully\n", action)
	}
}

// Run starts the service in daemon mode (called by the service manager).
func Run(s service.Service) {
	if err := s.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "service run failed: %v\n", err)
		os.Exit(1)
	}
}
