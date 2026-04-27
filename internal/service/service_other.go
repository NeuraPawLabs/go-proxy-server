//go:build !linux && !darwin

package service

import "fmt"

type unsupportedManager struct{}

func newManagerForRuntimeOS(_ string) Manager {
	return unsupportedManager{}
}

func (unsupportedManager) Install(ServiceSpec) error {
	return fmt.Errorf("service management is not supported on this platform")
}

func (unsupportedManager) Uninstall(string) error {
	return fmt.Errorf("service management is not supported on this platform")
}

func (unsupportedManager) Start(string) error {
	return fmt.Errorf("service management is not supported on this platform")
}

func (unsupportedManager) Stop(string) error {
	return fmt.Errorf("service management is not supported on this platform")
}

func (unsupportedManager) Status(string) (Status, error) {
	return Status{}, fmt.Errorf("service management is not supported on this platform")
}
