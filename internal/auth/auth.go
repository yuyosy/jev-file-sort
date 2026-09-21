package auth

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/zalando/go-keyring"
)

const (
	service = "jev-file-sort"
	account = "typesafe-api-key"
	EnvName = "TYPESAFE_API_KEY"
)

func Resolve() (value, source string, err error) {
	if value := strings.TrimSpace(os.Getenv(EnvName)); value != "" {
		return value, "environment", nil
	}
	value, err = keyring.Get(service, account)
	if errors.Is(err, keyring.ErrNotFound) {
		return "", "", fmt.Errorf("no API key configured; set %s or run auth login", EnvName)
	}
	if err != nil {
		return "", "", fmt.Errorf("read OS credential store: %w", err)
	}
	if strings.TrimSpace(value) == "" {
		return "", "", fmt.Errorf("stored API key is empty")
	}
	return value, "credential_store", nil
}

func Set(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return fmt.Errorf("API key must not be empty")
	}
	if err := keyring.Set(service, account, value); err != nil {
		return fmt.Errorf("write OS credential store: %w", err)
	}
	return nil
}

func Delete() error {
	err := keyring.Delete(service, account)
	if errors.Is(err, keyring.ErrNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("delete OS credential: %w", err)
	}
	return nil
}

func Status() (string, bool, error) {
	if strings.TrimSpace(os.Getenv(EnvName)) != "" {
		return "environment", true, nil
	}
	value, err := keyring.Get(service, account)
	if errors.Is(err, keyring.ErrNotFound) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("read OS credential store: %w", err)
	}
	return "credential_store", strings.TrimSpace(value) != "", nil
}
