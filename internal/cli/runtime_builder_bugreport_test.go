package cli

import (
	"reflect"
	"testing"

	configpkg "cyber-code/internal/config"
)

func TestConfiguredBugReportSecretsCollectsCredentialsWithoutNamesOrDuplicates(t *testing.T) {
	configuration := configpkg.Config{Profiles: map[string]configpkg.Profile{
		"one":       {APIKeyEnv: "ONE_KEY"},
		"two":       {APIKeyEnv: "TWO_KEY"},
		"duplicate": {APIKeyEnv: "DUPLICATE_KEY"},
	}}
	values := map[string]string{"ONE_KEY": "unlabeled-one", "TWO_KEY": "", "DUPLICATE_KEY": "unlabeled-one"}
	got := configuredBugReportSecrets(configuration, func(name string) (string, bool) {
		value, ok := values[name]
		return value, ok
	})
	if !reflect.DeepEqual(got, []string{"unlabeled-one"}) {
		t.Fatalf("secrets = %#v", got)
	}
}
