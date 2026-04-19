//go:build windows

package main

import (
	"context"
	"log/slog"

	"golang.org/x/sys/windows/svc"
)

const serviceName = "SoHoLINKAgent"

// agentService implements svc.Handler so the agent binary can run as a
// Windows service. The Execute method bridges the service control manager
// signals into the existing context-based shutdown mechanism.
type agentService struct {
	cancel context.CancelFunc
}

func (s *agentService) Execute(_ []string, req <-chan svc.ChangeRequest, status chan<- svc.Status) (bool, uint32) {
	status <- svc.Status{State: svc.StartPending}
	status <- svc.Status{
		State:   svc.Running,
		Accepts: svc.AcceptStop | svc.AcceptShutdown,
	}

	for cr := range req {
		switch cr.Cmd {
		case svc.Stop, svc.Shutdown:
			status <- svc.Status{State: svc.StopPending}
			s.cancel()
			return false, 0
		default:
			slog.Warn("unexpected service control command", "cmd", cr.Cmd)
		}
	}
	return false, 0
}

// runAsService starts the Windows service control loop. It calls runMain
// with a cancellable context so Stop/Shutdown signals propagate cleanly.
func runAsService() error {
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		runMain(ctx)
		cancel()
	}()
	return svc.Run(serviceName, &agentService{cancel: cancel})
}
