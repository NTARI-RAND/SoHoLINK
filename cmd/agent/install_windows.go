//go:build windows

package main

import (
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows/svc/mgr"
)

// installService registers the agent binary as a Windows service set to
// auto-start. Safe to call if the service already exists — it updates the
// config. Requires administrator privileges.
func installService() error {
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("install service: locate executable: %w", err)
	}
	exePath, err = filepath.Abs(exePath)
	if err != nil {
		return fmt.Errorf("install service: abs path: %w", err)
	}

	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("install service: connect to SCM: %w", err)
	}
	defer m.Disconnect() //nolint:errcheck

	s, err := m.OpenService(serviceName)
	if err == nil {
		// Service already exists — update config and exit.
		defer s.Close()
		cfg, cfgErr := s.Config()
		if cfgErr != nil {
			return fmt.Errorf("install service: read config: %w", cfgErr)
		}
		cfg.BinaryPathName = exePath
		cfg.StartType = mgr.StartAutomatic
		cfg.DisplayName = "SoHoLINK Node Agent"
		cfg.Description = "Contributes compute capacity to the SoHoLINK network."
		return s.UpdateConfig(cfg)
	}

	s, err = m.CreateService(serviceName, exePath, mgr.Config{
		StartType:   mgr.StartAutomatic,
		DisplayName: "SoHoLINK Node Agent",
		Description: "Contributes compute capacity to the SoHoLINK network.",
	})
	if err != nil {
		return fmt.Errorf("install service: create: %w", err)
	}
	defer s.Close()
	return nil
}

// removeService stops and removes the Windows service. Requires administrator
// privileges. Returns nil if the service does not exist.
func removeService() error {
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("remove service: connect to SCM: %w", err)
	}
	defer m.Disconnect() //nolint:errcheck

	s, err := m.OpenService(serviceName)
	if err != nil {
		// Service not installed — nothing to do.
		return nil
	}
	defer s.Close()

	if err := s.Delete(); err != nil {
		return fmt.Errorf("remove service: delete: %w", err)
	}
	return nil
}
