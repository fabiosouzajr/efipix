package secrets

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEnvProviderCredentials(t *testing.T) {
	dir := t.TempDir()
	certPath := filepath.Join(dir, "cert.pem")
	keyPath := filepath.Join(dir, "key.pem")
	require.NoError(t, os.WriteFile(certPath, []byte("CERTDATA"), 0o600))
	require.NoError(t, os.WriteFile(keyPath, []byte("KEYDATA"), 0o600))

	js := `{"prov-1":{"client_id":"cid","client_secret":"sec","cert_pem_path":"` +
		certPath + `","key_pem_path":"` + keyPath + `","sandbox":true}}`
	t.Setenv("EFI_CREDENTIALS", js)

	sp, err := NewEnv()
	require.NoError(t, err)
	c, err := sp.ProviderCredentials(context.Background(), "prov-1")
	require.NoError(t, err)
	require.Equal(t, "cid", c.ClientID)
	require.Equal(t, "sec", c.ClientSecret)
	require.Equal(t, []byte("CERTDATA"), c.CertPEM)
	require.Equal(t, []byte("KEYDATA"), c.KeyPEM)
	require.True(t, c.Sandbox)

	_, err = sp.ProviderCredentials(context.Background(), "missing")
	require.Error(t, err)
}
