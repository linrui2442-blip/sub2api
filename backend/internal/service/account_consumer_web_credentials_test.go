package service

import (
	"errors"
	"strings"
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

type recordingConsumerWebEncryptor struct {
	values []string
}

func (e *recordingConsumerWebEncryptor) Encrypt(v string) (string, error) {
	e.values = append(e.values, v)
	return "cipher:" + v, nil
}

func (e *recordingConsumerWebEncryptor) Decrypt(v string) (string, error) {
	return strings.TrimPrefix(v, "cipher:"), nil
}

func TestNormalizeConsumerWebCookieValueReferenceParityAndValidation(t *testing.T) {
	tests := []struct {
		name       string
		value      *string
		want       *string
		wantErr    bool
		secretText string
	}{
		{name: "field absent", value: nil, want: nil},
		{name: "raw plain value", value: stringPtr("fakevalue"), want: stringPtr("fakevalue")},
		{name: "leading whitespace", value: stringPtr("  fakevalue"), want: stringPtr("fakevalue")},
		{name: "trailing whitespace", value: stringPtr("fakevalue\t "), want: stringPtr("fakevalue")},
		{name: "outer double quotes", value: stringPtr(`"fakevalue"`), want: stringPtr("fakevalue")},
		{name: "outer single quotes", value: stringPtr("'fakevalue'"), want: stringPtr("fakevalue")},
		{name: "repeated double quotes", value: stringPtr(`"""fakevalue"""`), want: stringPtr("fakevalue")},
		{name: "repeated single quotes", value: stringPtr("'''fakevalue'''"), want: stringPtr("fakevalue")},
		{name: "whitespace then quotes", value: stringPtr(" \t\"fakevalue\" \r\n"), want: stringPtr("fakevalue")},
		{name: "source order does not retrim revealed whitespace", value: stringPtr(`" fakevalue "`), want: stringPtr(" fakevalue ")},
		{name: "whitespace only", value: stringPtr(" \t\r\n "), wantErr: true},
		{name: "quotes only", value: stringPtr(`""`), wantErr: true},
		{name: "PSID name value", value: stringPtr("__Secure-1PSID=fakevalue"), wantErr: true, secretText: "fakevalue"},
		{name: "PSIDTS name value", value: stringPtr("__Secure-1PSIDTS=fakevalue"), wantErr: true, secretText: "fakevalue"},
		{name: "cookie header lower", value: stringPtr("cookie: fakevalue"), wantErr: true, secretText: "fakevalue"},
		{name: "cookie header mixed case", value: stringPtr("CoOkIe: fakevalue"), wantErr: true, secretText: "fakevalue"},
		{name: "embedded CR", value: stringPtr("fake\rvalue"), wantErr: true, secretText: "fake"},
		{name: "embedded LF", value: stringPtr("fake\nvalue"), wantErr: true, secretText: "fake"},
		{name: "one trailing semicolon", value: stringPtr("fakevalue;"), want: stringPtr("fakevalue")},
		{name: "two trailing semicolons", value: stringPtr("fakevalue;;"), wantErr: true, secretText: "fakevalue"},
		{name: "embedded semicolon", value: stringPtr("fake;value"), wantErr: true, secretText: "fake"},
		{name: "percent sequence unchanged", value: stringPtr("fake%2Fvalue"), want: stringPtr("fake%2Fvalue")},
		{name: "ordinary equals allowed", value: stringPtr("fake=value"), want: stringPtr("fake=value")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := normalizeConsumerWebCookieValue("__Secure-1PSID", tt.value)
			if tt.wantErr {
				require.Error(t, err)
				require.ErrorIs(t, err, ErrInvalidConsumerWebCredential)
				require.Nil(t, got)
				if tt.secretText != "" {
					require.NotContains(t, err.Error(), tt.secretText)
				}
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestEncryptConsumerWebCredentialPatchNormalizesAllBeforeEncryption(t *testing.T) {
	enc := &recordingConsumerWebEncryptor{}
	psid := `  """fake-psid"""  `
	psidts := " '''fake-psidts''' "
	patch, err := EncryptConsumerWebCredentialPatch(enc, &psid, &psidts)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"fake-psid", "fake-psidts"}, enc.values)
	require.Equal(t, "cipher:fake-psid", patch[ConsumerWebPSIDCiphertextKey])
	require.Equal(t, "cipher:fake-psidts", patch[ConsumerWebPSIDTSCiphertextKey])

	enc.values = nil
	invalid := "__Secure-1PSIDTS=fake-secret-that-must-not-appear"
	patch, err = EncryptConsumerWebCredentialPatch(enc, &psid, &invalid)
	require.ErrorIs(t, err, ErrInvalidConsumerWebCredential)
	require.NotContains(t, err.Error(), "fake-secret-that-must-not-appear")
	require.Nil(t, patch)
	require.Empty(t, enc.values, "both fields must validate before either is encrypted")
}

func TestEncryptConsumerWebCredentialPatchPartialUpdatesAndPreservation(t *testing.T) {
	enc := &recordingConsumerWebEncryptor{}
	existing := map[string]any{
		"access_token": "oauth",
		"project_id":   "project",
		ConsumerWebCredentialNamespace: map[string]any{
			ConsumerWebPSIDCiphertextKey:   "cipher:old-psid",
			ConsumerWebPSIDTSCiphertextKey: "cipher:old-psidts",
			"future_field":                 "preserve-me",
		},
	}

	psid := " new-psid "
	patch, err := EncryptConsumerWebCredentialPatch(enc, &psid, nil)
	require.NoError(t, err)
	out := UpdateAccountCredentialNamespace(existing, ConsumerWebCredentialNamespace, patch)
	ns := out[ConsumerWebCredentialNamespace].(map[string]any)
	require.Equal(t, "cipher:new-psid", ns[ConsumerWebPSIDCiphertextKey])
	require.Equal(t, "cipher:old-psidts", ns[ConsumerWebPSIDTSCiphertextKey])
	require.Equal(t, "preserve-me", ns["future_field"])
	require.Equal(t, "oauth", out["access_token"])
	require.Equal(t, "project", out["project_id"])

	psidts := " new-psidts "
	patch, err = EncryptConsumerWebCredentialPatch(enc, nil, &psidts)
	require.NoError(t, err)
	out = UpdateAccountCredentialNamespace(existing, ConsumerWebCredentialNamespace, patch)
	ns = out[ConsumerWebCredentialNamespace].(map[string]any)
	require.Equal(t, "cipher:old-psid", ns[ConsumerWebPSIDCiphertextKey])
	require.Equal(t, "cipher:new-psidts", ns[ConsumerWebPSIDTSCiphertextKey])

	patch, err = EncryptConsumerWebCredentialPatch(enc, nil, nil)
	require.NoError(t, err)
	require.Empty(t, patch)
}

func TestNormalizeConsumerWebCookieValueRejectsEitherCookiePrefixInEitherField(t *testing.T) {
	for _, field := range []string{"__Secure-1PSID", "__Secure-1PSIDTS"} {
		for _, prefix := range []string{"__Secure-1PSID=", "__Secure-1PSIDTS="} {
			value := prefix + "cross-field-fake-secret"
			got, err := normalizeConsumerWebCookieValue(field, &value)
			require.ErrorIs(t, err, ErrInvalidConsumerWebCredential)
			require.NotContains(t, err.Error(), "cross-field-fake-secret")
			require.Nil(t, got)
		}
	}
}

func stringPtr(value string) *string { return &value }
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
