package main

import (
	"context"
	"fmt"
	"os"

	"github.com/kardianos/service"
)

var serviceConfig = &service.Config{
	Name:        "surge-web",
	DisplayName: "Surge Web Dashboard",
	Description: "Web-based dashboard for the Surge download manager.",
	Arguments:   []string{"run"},
}

func setServiceArgs(args []string) {
	serviceConfig.Arguments = append([]string{"run"}, args...)
}

type program struct {
	exit   chan struct{}
	cancel context.CancelFunc
}

func (p *program) Start(s service.Service) error {
	ctx, cancel := context.WithCancel(context.Background())
	p.cancel = cancel
	p.exit = make(chan struct{})

	go func() {
		defer close(p.exit)
		// Replay main with the original arguments passed to the binary,
		// minus the service runner wrapper. The service manager has
		// already set up working directory, env, etc.
		runServer(ctx)
	}()

	return nil
}

func (p *program) Stop(s service.Service) error {
	if p.cancel != nil {
		p.cancel()
	}
	if p.exit != nil {
		<-p.exit
	}
	return nil
}

func getService() (service.Service, error) {
	return service.New(&program{}, serviceConfig)
}

func runServiceCommand(action string) {
	s, err := getService()
	if err != nil {
		fmt.Fprintf(os.Stderr, "service error: %v\n", err)
		os.Exit(1)
	}

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

func runAsService() {
	s, err := getService()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create service: %v\n", err)
		os.Exit(1)
	}
	if err := s.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "service run failed: %v\n", err)
		os.Exit(1)
	}
}
