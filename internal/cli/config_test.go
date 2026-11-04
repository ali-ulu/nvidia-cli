package cli

import "testing"

func TestAPIKeyPrefersFlag(t *testing.T) {
	t.Setenv(EnvAPIKey, "from-env")
	got, err := APIKey("from-flag")
	if err != nil || got != "from-flag" {
		t.Fatalf("APIKey = %q, %v; want from-flag", got, err)
	}
}

func TestAPIKeyFallsBackToEnv(t *testing.T) {
	t.Setenv(EnvAPIKey, "from-env")
	got, err := APIKey("")
	if err != nil || got != "from-env" {
		t.Fatalf("APIKey = %q, %v; want from-env", got, err)
	}
}

func TestAPIKeyErrorsWhenMissing(t *testing.T) {
	t.Setenv(EnvAPIKey, "")
	if _, err := APIKey(""); err == nil {
		t.Fatal("want error when neither flag nor env is set")
	}
}
