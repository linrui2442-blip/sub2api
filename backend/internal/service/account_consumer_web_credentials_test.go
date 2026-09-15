package service

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

type consumerWebTestEncryptor struct{ fail bool }

func (e consumerWebTestEncryptor) Encrypt(v string) (string, error) {
	if e.fail {
		return "", errors.New("fail")
	}
	return "cipher:" + v, nil
}
func (e consumerWebTestEncryptor) Decrypt(v string) (string, error) {
	if len(v) < 7 {
		return "", errors.New("bad")
	}
	return v[7:], nil
}

func TestUpdateAccountCredentialNamespacePreservesUnrelatedAndNestedValues(t *testing.T) {
	existing := map[string]any{"access_token": "oauth", "consumer_web": map[string]any{ConsumerWebPSIDCiphertextKey: "old-a", ConsumerWebPSIDTSCiphertextKey: "old-b"}}
	out := UpdateAccountCredentialNamespace(existing, ConsumerWebCredentialNamespace, map[string]any{ConsumerWebPSIDTSCiphertextKey: "new-b", ConsumerWebPSIDCiphertextKey: nil})
	require.Equal(t, "oauth", out["access_token"])
	ns := out[ConsumerWebCredentialNamespace].(map[string]any)
	require.Equal(t, "old-a", ns[ConsumerWebPSIDCiphertextKey])
	require.Equal(t, "new-b", ns[ConsumerWebPSIDTSCiphertextKey])
	require.Equal(t, "old-b", existing[ConsumerWebCredentialNamespace].(map[string]any)[ConsumerWebPSIDTSCiphertextKey])
}

func TestConsumerWebCredentialEncryptionAndRotation(t *testing.T) {
	enc := consumerWebTestEncryptor{}
	psid, psidts := "test-psid-secret", "test-psidts-secret"
	patch, err := EncryptConsumerWebCredentialPatch(enc, &psid, &psidts)
	require.NoError(t, err)
	require.NotEqual(t, psid, patch[ConsumerWebPSIDCiphertextKey])
	credentials := UpdateAccountCredentialNamespace(map[string]any{"project_id": "p"}, ConsumerWebCredentialNamespace, patch)
	gotPSID, gotPSIDTS, err := DecryptConsumerWebCredentials(enc, credentials)
	require.NoError(t, err)
	require.Equal(t, psid, gotPSID)
	require.Equal(t, psidts, gotPSIDTS)
	rotated, err := UpdateConsumerWebPSIDTS(credentials, enc, "rotated")
	require.NoError(t, err)
	require.Equal(t, credentials[ConsumerWebCredentialNamespace].(map[string]any)[ConsumerWebPSIDCiphertextKey], rotated[ConsumerWebCredentialNamespace].(map[string]any)[ConsumerWebPSIDCiphertextKey])
	require.Equal(t, "p", rotated["project_id"])
}

func TestConsumerWebCredentialEncryptionFailureIsAtomic(t *testing.T) {
	psid, psidts := "test-psid-secret", "test-psidts-secret"
	patch, err := EncryptConsumerWebCredentialPatch(consumerWebTestEncryptor{fail: true}, &psid, &psidts)
	require.Error(t, err)
	require.Nil(t, patch)
}

func TestMergeCredentialsPreservesConsumerWebAndOAuthMetadata(t *testing.T) {
	ns := map[string]any{ConsumerWebPSIDCiphertextKey: "cipher-a"}
	old := map[string]any{"access_token": "old", "refresh_token": "refresh", "project_id": "project", "paid_tier": "pro", "plan_type": "pro", ConsumerWebCredentialNamespace: ns}
	out := MergeCredentials(old, map[string]any{"access_token": "new"})
	require.Equal(t, "new", out["access_token"])
	for _, key := range []string{"refresh_token", "project_id", "paid_tier", "plan_type", ConsumerWebCredentialNamespace} {
		require.Equal(t, old[key], out[key])
	}
}
