package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadEnvFileSetsMissingEnvironmentVariables(t *testing.T) {
	tempDir := t.TempDir()
	envPath := filepath.Join(tempDir, ".env")
	content := "GEETEST_ID=test-id\nexport GEETEST_KEY=\"test-key\"\nEMPTY_VALUE=\nSINGLE_QUOTED='quoted value'\n"
	if err := os.WriteFile(envPath, []byte(content), 0600); err != nil {
		t.Fatalf("write .env: %v", err)
	}

	t.Setenv("GEETEST_ID", "")
	if err := os.Unsetenv("GEETEST_ID"); err != nil {
		t.Fatalf("unset GEETEST_ID: %v", err)
	}
	if err := os.Unsetenv("GEETEST_KEY"); err != nil {
		t.Fatalf("unset GEETEST_KEY: %v", err)
	}
	if err := os.Unsetenv("EMPTY_VALUE"); err != nil {
		t.Fatalf("unset EMPTY_VALUE: %v", err)
	}
	if err := os.Unsetenv("SINGLE_QUOTED"); err != nil {
		t.Fatalf("unset SINGLE_QUOTED: %v", err)
	}

	if err := loadEnvFile(envPath); err != nil {
		t.Fatalf("loadEnvFile: %v", err)
	}

	if got := os.Getenv("GEETEST_ID"); got != "test-id" {
		t.Fatalf("unexpected GEETEST_ID: got %q want %q", got, "test-id")
	}
	if got := os.Getenv("GEETEST_KEY"); got != "test-key" {
		t.Fatalf("unexpected GEETEST_KEY: got %q want %q", got, "test-key")
	}
	if got := os.Getenv("EMPTY_VALUE"); got != "" {
		t.Fatalf("unexpected EMPTY_VALUE: got %q want empty", got)
	}
	if got := os.Getenv("SINGLE_QUOTED"); got != "quoted value" {
		t.Fatalf("unexpected SINGLE_QUOTED: got %q want %q", got, "quoted value")
	}
}

func TestLoadEnvFileDoesNotOverrideExistingEnvironmentVariables(t *testing.T) {
	tempDir := t.TempDir()
	envPath := filepath.Join(tempDir, ".env")
	if err := os.WriteFile(envPath, []byte("GEETEST_ID=file-value\n"), 0600); err != nil {
		t.Fatalf("write .env: %v", err)
	}

	t.Setenv("GEETEST_ID", "existing-value")

	if err := loadEnvFile(envPath); err != nil {
		t.Fatalf("loadEnvFile: %v", err)
	}

	if got := os.Getenv("GEETEST_ID"); got != "existing-value" {
		t.Fatalf("existing env var should win: got %q want %q", got, "existing-value")
	}
}

func TestLoadEnvFileRejectsMalformedLines(t *testing.T) {
	tempDir := t.TempDir()
	envPath := filepath.Join(tempDir, ".env")
	if err := os.WriteFile(envPath, []byte("NOT_VALID\n"), 0600); err != nil {
		t.Fatalf("write .env: %v", err)
	}

	if err := loadEnvFile(envPath); err == nil {
		t.Fatal("expected malformed .env to fail")
	}
}
