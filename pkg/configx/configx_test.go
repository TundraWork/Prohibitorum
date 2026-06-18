package configx

import (
	"encoding/base64"
	"os"
	"testing"
	"time"

	"github.com/spf13/viper"
)

// resetViper isolates each test from the package-global viper state and the
// env vars Parse() reads. The DATA_ENCRYPTION_KEY var is required by Parse so
// every subtest sets it before running.
func resetViper(t *testing.T) {
	t.Helper()
	viper.Reset()
	// Strip any inherited prohibitorum env so the test owns the surface.
	for _, kv := range os.Environ() {
		for _, prefix := range []string{"PROHIBITORUM_"} {
			if len(kv) >= len(prefix) && kv[:len(prefix)] == prefix {
				// kv looks like KEY=VALUE
				eq := -1
				for i := 0; i < len(kv); i++ {
					if kv[i] == '=' {
						eq = i
						break
					}
				}
				if eq > 0 {
					t.Setenv(kv[:eq], "")
				}
			}
		}
	}
	// Provide a key so Parse doesn't reject the config outright.
	t.Setenv("PROHIBITORUM_DATA_ENCRYPTION_KEY_V1", base64.StdEncoding.EncodeToString(make([]byte, 32)))
}

func TestParse_AuthDefaults(t *testing.T) {
	resetViper(t)
	cfg, err := Parse()
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	wantSchedule := []time.Duration{
		0, 0,
		time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second,
		16 * time.Second, 32 * time.Second,
		time.Minute, 2 * time.Minute, 4 * time.Minute, 8 * time.Minute,
		15 * time.Minute,
	}
	if len(cfg.Auth.ThrottleSchedule) != len(wantSchedule) {
		t.Fatalf("ThrottleSchedule length: want %d, got %d (%v)",
			len(wantSchedule), len(cfg.Auth.ThrottleSchedule), cfg.Auth.ThrottleSchedule)
	}
	for i, w := range wantSchedule {
		if cfg.Auth.ThrottleSchedule[i] != w {
			t.Errorf("ThrottleSchedule[%d]: want %v, got %v", i, w, cfg.Auth.ThrottleSchedule[i])
		}
	}

	if cfg.Auth.SudoTTL != 15*time.Minute {
		t.Errorf("Auth.SudoTTL: want 15m, got %v", cfg.Auth.SudoTTL)
	}
	if cfg.Auth.PartialSessionTTL != 5*time.Minute {
		t.Errorf("Auth.PartialSessionTTL: want 5m, got %v", cfg.Auth.PartialSessionTTL)
	}
}

func TestParse_PasswordHashParallelismDefault(t *testing.T) {
	resetViper(t)
	cfg, err := Parse()
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if cfg.PasswordHashParams.Parallelism != 1 {
		t.Errorf("PasswordHashParams.Parallelism default: want 1, got %d", cfg.PasswordHashParams.Parallelism)
	}
	// Memory/iteration defaults must not regress.
	if cfg.PasswordHashParams.MemoryKiB != 65536 {
		t.Errorf("PasswordHashParams.MemoryKiB: want 65536, got %d", cfg.PasswordHashParams.MemoryKiB)
	}
	if cfg.PasswordHashParams.Iterations != 3 {
		t.Errorf("PasswordHashParams.Iterations: want 3, got %d", cfg.PasswordHashParams.Iterations)
	}
}

func TestParse_TOTPIssuerFallsBackToRPDisplayName(t *testing.T) {
	resetViper(t)
	cfg, err := Parse()
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	// totp.issuer is left blank by default → must resolve to the (also
	// defaulted) webauthn.rp_display_name = "Prohibitorum".
	if cfg.TOTP.Issuer != "Prohibitorum" {
		t.Errorf("TOTP.Issuer fallback: want %q, got %q", "Prohibitorum", cfg.TOTP.Issuer)
	}
}

func TestParse_TOTPIssuerExplicitOverride(t *testing.T) {
	resetViper(t)
	t.Setenv("PROHIBITORUM_TOTP_ISSUER", "Acme Corp")
	t.Setenv("PROHIBITORUM_WEBAUTHN_RP_DISPLAY_NAME", "Acme Auth")
	cfg, err := Parse()
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if cfg.TOTP.Issuer != "Acme Corp" {
		t.Errorf("explicit totp.issuer must win over rp_display_name fallback: got %q", cfg.TOTP.Issuer)
	}
}
